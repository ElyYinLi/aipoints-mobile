package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
	"github.com/ElyYinLi/aipoints-mobile/core/provider"
	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// ---------- task_memo ----------

func TestTaskKeyIsolatesProviderAndAccount(t *testing.T) {
	a := taskKey("2026-11-20", "aipy", "acc1", "daily_ai_1")
	b := taskKey("2026-11-20", "dumate", "acc1", "daily_ai_1")
	c := taskKey("2026-11-20", "aipy", "acc2", "daily_ai_1")
	d := taskKey("2026-11-21", "aipy", "acc1", "daily_ai_1")
	seen := map[string]bool{a: true, b: true, c: true, d: true}
	if len(seen) != 4 {
		t.Fatal("provider/account/date 任一维度不同都应生成不同的键，" +
			"否则两个平台同名的任务会互相顶掉")
	}
}

func TestTaskMemoRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := newTaskMemo(dir)
	key := taskMemoKey("aipy", "acc1", "daily_ai_1")
	if m.seen(key) {
		t.Fatal("新键不应命中")
	}
	m.mark(key)
	if !m.seen(key) {
		t.Fatal("标记后应命中")
	}

	// 重开（模拟重启 App）后仍要记得住，否则重启就重复申领
	m2 := newTaskMemo(dir)
	if !m2.seen(key) {
		t.Fatal("落盘后重开应仍命中")
	}
	// 别的键不受影响
	if m2.seen(taskMemoKey("aipy", "acc1", "other")) {
		t.Fatal("不同任务不应互相污染")
	}
}

// 昨天的键在重开时被清掉，文件不会越滚越大。
func TestTaskMemoDropsStaleDays(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task_memo.json")
	old := map[string]bool{
		"2020-01-01|aipy|acc1|daily_ai_1":   true,
		taskMemoKey("aipy", "acc1", "keep"): true,
	}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTaskMemo(dir)
	if m.seen("2020-01-01|aipy|acc1|daily_ai_1") {
		t.Fatal("过期日期的键应被清理")
	}
	if !m.seen(taskMemoKey("aipy", "acc1", "keep")) {
		t.Fatal("今天的键应保留")
	}
}

// ---------- 任务领取去重（端到端） ----------

// 上游 status 故意滞后（永远返回 locked + 可领），必须靠本地去重挡住第二次申领。
func TestTaskClaimDedupedByLocalMemo(t *testing.T) {
	var claimCount int
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/user/daily_tasks/claim" && r.Method == "POST" {
			claimCount++
			w.Write([]byte(`{"success":true,"data":{"reward":50000}}`))
			return
		}
		w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer up.Close()

	reg := provider.NewRegistry(httpx.New())
	fs := fakeStoreWithAccounts{accounts: []store.Account{{
		Provider: "aipy", AccountID: "a1", Enabled: true,
		SecretJSON: json.RawMessage(`{"cookie":"s=1","domain":"` + up.URL + `"}`),
	}}}
	s := New(Options{Store: fs, Reg: reg, DataDir: t.TempDir()})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/claim", s.handleTaskClaim)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	post := func() map[string]any {
		body := strings.NewReader(`{"provider":"aipy","account":"a1","task_type":"daily_ai_1"}`)
		resp, err := http.Post(srv.URL+"/api/tasks/claim", "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	first := post()
	if ok, _ := first["ok"].(bool); !ok {
		t.Fatalf("首次领取应成功: %v", first)
	}
	if claimCount != 1 {
		t.Fatalf("首次应发 1 次 claim，实际 %d", claimCount)
	}

	second := post()
	if deduped, _ := second["deduped"].(bool); !deduped {
		t.Fatalf("第二次应被本地去重: %v", second)
	}
	if claimCount != 1 {
		t.Fatalf("去重后不应再打上游，实际发了 %d 次", claimCount)
	}

	// ignore_memo 可强制放行（给「上游真的漏发了」的补偿场景留口子）
	third := postWithIgnore(t, srv.URL)
	if third == nil {
		t.Fatal("ignore_memo 请求失败")
	}
	if claimCount != 2 {
		t.Fatalf("ignore_memo 应放行一次，实际 claim=%d", claimCount)
	}
}

func postWithIgnore(t *testing.T, base string) map[string]any {
	t.Helper()
	body := strings.NewReader(`{"provider":"aipy","account":"a1","task_type":"daily_ai_1","ignore_memo":true}`)
	resp, err := http.Post(base+"/api/tasks/claim", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// 上游返回 0 奖励时不能记账——否则用户以为到账，是假账。
func TestTaskClaimZeroRewardNotRecorded(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"reward":0}}`))
	}))
	defer up.Close()

	reg := provider.NewRegistry(httpx.New())
	fs := fakeStoreWithAccounts{accounts: []store.Account{{
		Provider: "aipy", AccountID: "a1", Enabled: true,
		SecretJSON: json.RawMessage(`{"cookie":"s=1","domain":"` + up.URL + `"}`),
	}}}
	s := New(Options{Store: fs, Reg: reg, DataDir: t.TempDir()})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/claim", s.handleTaskClaim)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := strings.NewReader(`{"provider":"aipy","account":"a1","task_type":"t1"}`)
	resp, err := http.Post(srv.URL+"/api/tasks/claim", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if s.tasks.seen(taskMemoKey("aipy", "a1", "t1")) {
		t.Fatal("0 奖励不应记为已领取")
	}
	// 再点一次应该仍会去打上游（因为没记账）
	body2 := strings.NewReader(`{"provider":"aipy","account":"a1","task_type":"t1"}`)
	resp2, err := http.Post(srv.URL+"/api/tasks/claim", "application/json", body2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&out)
	if deduped, _ := out["deduped"].(bool); deduped {
		t.Fatal("0 奖励那次没记账，第二次不应被判为已领过")
	}
}

// ---------- 令牌 ----------

func TestProvidersMarkTokenSupport(t *testing.T) {
	reg := provider.NewRegistry(httpx.New())
	s := New(Options{Store: fakeStore{}, Reg: reg})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/providers", s.handleProviders)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	for _, p := range list {
		has, _ := p["tokens"].(bool)
		if p["id"] == "aipy" && !has {
			t.Fatalf("aipy 应标记 tokens=true: %v", list)
		}
		if p["id"] != "aipy" && has {
			t.Fatalf("%v 不应标记 tokens=true", p["id"])
		}
	}
}

func tokenTestSetup(t *testing.T, upstreamURL, accessToken string) (*Server, *httptest.Server) {
	t.Helper()
	reg := provider.NewRegistry(httpx.New())
	secret := `{"cookie":"s=1","domain":"` + upstreamURL + `"`
	if accessToken != "" {
		secret += `,"access_token":"` + accessToken + `"`
	}
	secret += `}`
	fs := fakeStoreWithAccounts{accounts: []store.Account{{
		Provider: "aipy", AccountID: "a1", Enabled: true, SecretJSON: json.RawMessage(secret),
	}}}
	s := New(Options{Store: fs, Reg: reg, DataDir: t.TempDir()})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tokens", s.handleTokens)
	mux.HandleFunc("/api/tokens/save", s.handleTokenSave)
	mux.HandleFunc("/api/tokens/delete", s.handleTokenDelete)
	mux.HandleFunc("/api/tokens/status", s.handleTokenStatus)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return s, srv
}

func TestTokensListFlow(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"items":[
			{"id":1,"name":"主令牌","key":"sk-aaa","status":1,"expired_time":-1}
		],"total":1}}`))
	}))
	defer up.Close()

	_, srv := tokenTestSetup(t, up.URL, "")
	resp, err := http.Get(srv.URL + "/api/tokens?provider=aipy&account=a1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("列表应成功: %v", out)
	}
	toks, _ := out["tokens"].([]any)
	if len(toks) != 1 {
		t.Fatalf("应返回 1 条令牌: %v", out)
	}
}

// 删除保护：待删令牌的 key 等于账号当前 access_token 时必须拒绝。
func TestTokenDeleteGuardInUse(t *testing.T) {
	var deleted bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deleted = true
		}
		w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer up.Close()

	_, srv := tokenTestSetup(t, up.URL, "sk-in-use")
	body := strings.NewReader(`{"provider":"aipy","account":"a1","id":9,"key":"sk-in-use"}`)
	resp, err := http.Post(srv.URL+"/api/tokens/delete", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)

	if ok, _ := out["ok"].(bool); ok {
		t.Fatalf("删正在用的令牌应被拒绝: %v", out)
	}
	if inUse, _ := out["in_use"].(bool); !inUse {
		t.Fatalf("应标记 in_use=true: %v", out)
	}
	if deleted {
		t.Fatal("被拒绝时不应真的发出 DELETE")
	}
}

func TestTokenDeleteAllowsOtherKey(t *testing.T) {
	var deletedPath string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletedPath = r.URL.Path
		}
		w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer up.Close()

	_, srv := tokenTestSetup(t, up.URL, "sk-in-use")
	body := strings.NewReader(`{"provider":"aipy","account":"a1","id":9,"key":"sk-other"}`)
	resp, err := http.Post(srv.URL+"/api/tokens/delete", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("删别的令牌应成功: %v", out)
	}
	if deletedPath != "/api/token/9/" {
		t.Fatalf("应发出 DELETE /api/token/9/，实际 %q", deletedPath)
	}
}

func TestTokenSaveRequiresName(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("空名称不应打上游")
	}))
	defer up.Close()
	_, srv := tokenTestSetup(t, up.URL, "")
	body := strings.NewReader(`{"provider":"aipy","account":"a1","token":{"name":"  "}}`)
	resp, err := http.Post(srv.URL+"/api/tokens/save", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); ok {
		t.Fatalf("空名称应报错: %v", out)
	}
}

func TestTokenStatusRejectsBadValue(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("非法 status 不应打上游")
	}))
	defer up.Close()
	_, srv := tokenTestSetup(t, up.URL, "")
	body := strings.NewReader(`{"provider":"aipy","account":"a1","id":1,"status":7}`)
	resp, err := http.Post(srv.URL+"/api/tokens/status", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); ok {
		t.Fatalf("status=7 应报错: %v", out)
	}
}

// 不支持的平台直接给出可读错误，而不是 500。
func TestTokensUnsupportedProvider(t *testing.T) {
	_, srv := tokenTestSetup(t, "http://127.0.0.1:1", "")
	resp, err := http.Get(srv.URL + "/api/tokens?provider=lingxi&account=a1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); ok {
		t.Fatalf("lingxi 没有令牌管理，应返回失败: %v", out)
	}
}

// ---------- 通知 ----------

func TestNotifyConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	n := newNotifier(dir)
	if err := n.save(NotifyConfig{
		Enabled: true, Type: "dingtalk", URL: "https://example.com/hook",
		OnSuccess: true, OnFailure: true, OnManual: true,
	}); err != nil {
		t.Fatal(err)
	}
	n2 := newNotifier(dir)
	c := n2.config()
	if !c.Enabled || c.Type != "dingtalk" || c.URL != "https://example.com/hook" {
		t.Fatalf("配置未持久化: %+v", c)
	}
}

func TestNotifyRejectsBadTypeAndURL(t *testing.T) {
	n := newNotifier("")
	if err := n.save(NotifyConfig{Type: "telegram"}); err == nil {
		t.Fatal("未知渠道应报错")
	}
	if err := n.save(NotifyConfig{Type: "wecom", URL: "qyapi.weixin.qq.com"}); err == nil {
		t.Fatal("缺协议的地址应报错")
	}
}

// 五种渠道的报文格式各不相同，逐个比对关键字段。
func TestNotifyPayloadShapes(t *testing.T) {
	cases := []struct {
		kind string
		want []string
	}{
		{"wecom", []string{"msgtype", "text"}},
		{"dingtalk", []string{"msgtype", "text"}},
		{"feishu", []string{"msg_type", "content"}},
		{"bark", []string{"title", "body"}},
		{"custom", []string{"title", "text", "content"}},
	}
	for _, c := range cases {
		raw := notifyPayload(c.kind, "标题", "正文")
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s 报文不是合法 JSON: %v", c.kind, err)
		}
		for _, k := range c.want {
			if _, ok := m[k]; !ok {
				t.Fatalf("%s 报文缺字段 %s: %s", c.kind, k, string(raw))
			}
		}
	}
}

// 失败必须置顶：手机上一屏放不下 6 个账号。
func TestBuildTextPutsFailuresFirst(t *testing.T) {
	_, body := buildText(NotifyPayload{
		Trigger: "manual",
		Accounts: []NotifyAccountLine{
			{Provider: "aipy", Label: "A", Status: "ok", Message: "成功"},
			{Provider: "dumate", Label: "B", Status: "fail", Message: "登录失效"},
			{Provider: "trae", Label: "C", Status: "already", Message: "已签"},
		},
	})
	idxFail := strings.Index(body, "B：")
	idxOK := strings.Index(body, "A：")
	if idxFail < 0 || idxOK < 0 {
		t.Fatalf("正文缺少账号行: %s", body)
	}
	if idxFail > idxOK {
		t.Fatalf("失败行应排在成功行之前:\n%s", body)
	}
	if !strings.Contains(body, "❌") {
		t.Fatalf("失败行应带 ❌: %s", body)
	}
}

// 脱敏：sk- 密钥、Cookie、手机号、邮箱都不能出现在推送正文里。
func TestMaskNotifyStripsSecrets(t *testing.T) {
	in := "账号 sk-abcdef123456 手机 13812345678 邮箱 foo@bar.com Cookie: session=xyz"
	out := maskNotify(in)
	for _, bad := range []string{"abcdef123456", "13812345678", "foo@bar.com", "session=xyz"} {
		if strings.Contains(out, bad) {
			t.Fatalf("未脱敏 %q:\n%s", bad, out)
		}
	}
}

// send() 在未启用/无地址时静默跳过，不报错也不发请求。
func TestNotifySkipsWhenDisabled(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()
	n := newNotifier("")
	_ = n.save(NotifyConfig{Enabled: false, Type: "wecom", URL: srv.URL})
	err := n.send(context.Background(), NotifyPayload{Trigger: "manual"}, false)
	// 跳过要能被识别成「跳过」而不是「失败」：前端据此显示「没发」而不是报错
	if err == nil || !isSkip(err) {
		t.Fatalf("未启用时应返回可识别的跳过: %v", err)
	}
	if hit {
		t.Fatal("未启用时不应发请求")
	}
}

// 启动签到默认不推送，避免一天开几次 App 收几条。
func TestNotifySkipsStartTriggerByDefault(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()
	n := newNotifier("")
	_ = n.save(NotifyConfig{Enabled: true, Type: "wecom", URL: srv.URL, OnManual: true, OnSuccess: true})
	err := n.send(context.Background(), NotifyPayload{Trigger: "start"}, false)
	if !isSkip(err) {
		t.Fatalf("启动签到应被跳过: %v", err)
	}
	if hit {
		t.Fatal("on_start 默认关闭，启动签到不应推送")
	}
	// 手动触发则应该发
	if err := n.send(context.Background(), NotifyPayload{Trigger: "manual"}, false); err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("on_manual 开启时手动签到应推送")
	}
}

// 4xx（地址填错）要自动停用，避免每天无效重试。
func TestNotifyDisablesOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	dir := t.TempDir()
	n := newNotifier(dir)
	_ = n.save(NotifyConfig{Enabled: true, Type: "wecom", URL: srv.URL, OnManual: true, OnSuccess: true})
	if err := n.send(context.Background(), NotifyPayload{Trigger: "manual"}, false); err == nil {
		t.Fatal("404 应返回错误")
	}
	if n.config().Enabled {
		t.Fatal("4xx 后应自动停用")
	}
	if !newNotifier(dir).config().Enabled == true {
		// 停用状态也要落盘
		_ = newNotifier(dir)
	}
	st := n.status()
	if dis, _ := st["disabled"].(bool); !dis {
		t.Fatalf("应标记 disabled: %v", st)
	}
}

// 5xx 只记失败计数，不停用。
func TestNotifyCounts5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	n := newNotifier("")
	_ = n.save(NotifyConfig{Enabled: true, Type: "wecom", URL: srv.URL, OnManual: true, OnSuccess: true})
	_ = n.send(context.Background(), NotifyPayload{Trigger: "manual"}, false)
	st := n.status()
	if fs, _ := st["fail_streak"].(int); fs == 0 {
		t.Fatalf("5xx 应累加失败计数: %v", st)
	}
	if !n.config().Enabled {
		t.Fatal("5xx 不应自动停用")
	}
}

// handleNotify 在推送失败时也必须返回 ok=true——推送不能影响签到结果。
func TestHandleNotifyNeverFailsSignin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New()), DataDir: t.TempDir()})
	_ = s.notify.save(NotifyConfig{Enabled: true, Type: "wecom", URL: srv.URL, OnManual: true, OnSuccess: true})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notify", s.handleNotify)
	psrv := httptest.NewServer(mux)
	defer psrv.Close()

	body := strings.NewReader(`{"trigger":"manual","accounts":[],"summary":"x"}`)
	resp, err := http.Post(psrv.URL+"/api/notify", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("推送失败也要返回 ok=true: %v", out)
	}
	if sent, _ := out["sent"].(bool); sent {
		t.Fatal("本次应标记未发送成功")
	}
}

// 被配置过滤掉的一轮必须报 skipped，不能报 sent。
//
// 这条踩过：handleNotify 原先只判断 err == nil 就回 sent:true，
// 于是 on_start=false 挡下来的启动签到也显示「已发送」，用户以为收到过。
// 跳过与失败是两件事：跳过是用户自己关的，不该累计 fail_streak。
func TestHandleNotifyReportsSkippedNotSent(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New()), DataDir: t.TempDir()})
	// on_start 关闭：启动触发应被跳过
	_ = s.notify.save(NotifyConfig{Enabled: true, Type: "wecom", URL: srv.URL,
		OnManual: true, OnSuccess: true, OnStart: false})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/notify", s.handleNotify)
	psrv := httptest.NewServer(mux)
	defer psrv.Close()

	body := strings.NewReader(`{"trigger":"start","accounts":[{"provider":"aipy","label":"x","status":"ok","message":"ok"}],"summary":"s"}`)
	resp, err := http.Post(psrv.URL+"/api/notify", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)

	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("跳过也要回 ok=true（推送永不影响签到）: %v", out)
	}
	if sent, _ := out["sent"].(bool); sent {
		t.Fatalf("被过滤的一轮不能报已发送: %v", out)
	}
	if skipped, _ := out["skipped"].(bool); !skipped {
		t.Fatalf("应标记 skipped 以示「用户自己关的」: %v", out)
	}
	if hit {
		t.Fatal("on_start 关闭时不应真的发请求")
	}
	if fs := s.notify.status()["fail_streak"].(int); fs != 0 {
		t.Fatalf("跳过不能累计失败次数，实际 %d", fs)
	}
}

// ---------- busy 互斥 ----------

func TestBusyMutualExclusion(t *testing.T) {
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New())})
	if !s.acquire("aipy|a1") {
		t.Fatal("首次占用应成功")
	}
	if s.acquire("aipy|a1") {
		t.Fatal("同一账号第二次占用应失败")
	}
	if !s.acquire("aipy|a2") {
		t.Fatal("不同账号互不影响")
	}
	s.release("aipy|a1")
	if !s.acquire("aipy|a1") {
		t.Fatal("释放后应能重新占用")
	}
}

// 并发连点：只有第一个能拿到锁。
func TestBusyConcurrentAcquire(t *testing.T) {
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New())})
	const n = 8
	granted := make(chan bool, n)
	for i := 0; i < n; i++ {
		go func() { granted <- s.acquire("aipy|a1") }()
	}
	okCount := 0
	for i := 0; i < n; i++ {
		if <-granted {
			okCount++
		}
	}
	if okCount != 1 {
		t.Fatalf("并发 %d 次只应有 1 次拿到锁，实际 %d", n, okCount)
	}
}

// ---------- 二次核对 ----------

type confirmProvider struct {
	checkinOK   bool
	checkedIn   bool // Query 返回的今日签到状态
	queryErr    bool
	checkinCall int
}

func (c *confirmProvider) Meta() provider.Meta { return provider.Meta{ID: "fake"} }
func (c *confirmProvider) Query(ctx context.Context, cred provider.Credential) (provider.Snapshot, error) {
	if c.queryErr {
		return provider.Snapshot{Provider: "fake", Error: "网络错误"}, nil
	}
	return provider.Snapshot{Provider: "fake", OK: true, CheckedIn: c.checkedIn}, nil
}
func (c *confirmProvider) Checkin(ctx context.Context, cred provider.Credential) (provider.Result, error) {
	c.checkinCall++
	return provider.Result{Provider: "fake", OK: c.checkinOK, Gain: 10}, nil
}

// 上游说成功但回查没到账 → 报告「未确认到账」，不写成功。
func TestConfirmCheckinDetectsFakeSuccess(t *testing.T) {
	p := &confirmProvider{checkinOK: true, checkedIn: false}
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New())})
	res := s.checkinOne(context.Background(), p, store.Account{Provider: "fake", AccountID: "a1"}, provider.Credential{})
	if res.OK {
		t.Fatalf("回查未到账时不应报成功: %+v", res)
	}
	if !strings.Contains(res.Message, "未能确认到账") {
		t.Fatalf("应提示未确认到账: %s", res.Message)
	}
}

func TestConfirmCheckinPassesWhenReached(t *testing.T) {
	p := &confirmProvider{checkinOK: true, checkedIn: true}
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New())})
	res := s.checkinOne(context.Background(), p, store.Account{Provider: "fake", AccountID: "a1"}, provider.Credential{})
	if !res.OK {
		t.Fatalf("回查已到账应保持成功: %+v", res)
	}
}

// 回查本身失败（网络抖动）不能把成功误报成失败。
func TestConfirmCheckinToleratesQueryError(t *testing.T) {
	p := &confirmProvider{checkinOK: true, queryErr: true}
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New())})
	res := s.checkinOne(context.Background(), p, store.Account{Provider: "fake", AccountID: "a1"}, provider.Credential{})
	if !res.OK {
		t.Fatalf("回查失败不应判为没到账: %+v", res)
	}
}

// 已签（Already）不需要回查，省一次请求。
func TestConfirmSkipsWhenAlready(t *testing.T) {
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New())})
	p := &alreadyProvider{}
	r := s.checkinOne(context.Background(), p, store.Account{Provider: "fake2", AccountID: "a1"}, provider.Credential{})
	if !r.OK || !r.Already {
		t.Fatalf("已签应保持 ok+already: %+v", r)
	}
	if p.queryCall != 0 {
		t.Fatalf("已签不应触发回查，实际查了 %d 次", p.queryCall)
	}
}

type alreadyProvider struct{ queryCall int }

func (a *alreadyProvider) Meta() provider.Meta { return provider.Meta{ID: "fake2"} }
func (a *alreadyProvider) Query(ctx context.Context, cred provider.Credential) (provider.Snapshot, error) {
	a.queryCall++
	return provider.Snapshot{Provider: "fake2", OK: true}, nil
}
func (a *alreadyProvider) Checkin(ctx context.Context, cred provider.Credential) (provider.Result, error) {
	return provider.Result{Provider: "fake2", OK: true, Already: true, Message: "今日已签到"}, nil
}

// 确认 busy 键含 provider，避免两个平台同 id 互相锁。
func TestBusyKeyIncludesProvider(t *testing.T) {
	k1 := "aipy|same"
	k2 := "dumate|same"
	if k1 == k2 {
		t.Fatal("不同平台的同名账号不能共享锁")
	}
}

// usable 必须反映「这个账号能不能调令牌接口」，而不是「有没有令牌正在用」。
//
// 回归 0.6.0 的 bug：usable 写成 inUse != ""，而 Cookie 登录的账号本来就没有
// access_token，于是恒为 false，前端点「新建令牌」直接 return ——
// 用户看到的就是「点新建没反应」。
func TestTokensUsableWithCookieOnly(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"items":[]}}`))
	}))
	defer up.Close()

	// 只给 cookie，不给 access_token：这正是绝大多数用户的实际状态
	_, srv := tokenTestSetup(t, up.URL, "")
	resp, err := http.Get(srv.URL + "/api/tokens?provider=aipy&account=a1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if u, _ := out["usable"].(bool); !u {
		t.Fatalf("Cookie 登录的账号也应可管理令牌（usable=true），实际: %v", out)
	}
	if k, _ := out["in_use_key"].(string); k != "" {
		t.Fatalf("没有 access_token 时 in_use_key 应为空，实际: %q", k)
	}
}

// 列表必须把上游返回的 key 原样透出，不能在后端再做一次脱敏。
//
// 前端要靠这个字段显示与复制；后端若先截断，前端拿到的就是残缺串。
func TestTokensListKeepsFullKey(t *testing.T) {
	const full = "sk-abcdefghijklmnopqrstuvwxyz012345"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"items":[
			{"id":7,"name":"完整","key":"` + full + `","status":1,"expired_time":-1}
		]}}`))
	}))
	defer up.Close()

	_, srv := tokenTestSetup(t, up.URL, "")
	resp, err := http.Get(srv.URL + "/api/tokens?provider=aipy&account=a1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Tokens []provider.APIToken `json:"tokens"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Tokens) != 1 {
		t.Fatalf("应返回 1 条: %v", out)
	}
	if out.Tokens[0].Key != full {
		t.Fatalf("key 必须完整透出，期望 %q 实际 %q", full, out.Tokens[0].Key)
	}
}

// 全部已签的一轮也必须能上报，否则「开关全开却收不到推送」。
//
// 回归 0.6.1 的 bug：stat.lines 只在逐个签到的循环里填充，循环只遍历未签账号，
// 账号全签时 lines 为空，pushSummary 直接 return，请求根本没发出去。
// 前端兜底逻辑见 index.go 的 pushSummary；这里守住后端这一侧的语义：
// 「全部 already」不算失败，应被正常推送（always / on_manual 打开时）。
func TestNotifySendsWhenAllAlready(t *testing.T) {
	var hit int
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit++
		w.Write([]byte(`{}`))
	}))
	defer hook.Close()

	n := newNotifier("")
	if err := n.save(NotifyConfig{
		Enabled: true, Type: "wecom", URL: hook.URL,
		OnManual: true, Always: true,
	}); err != nil {
		t.Fatal(err)
	}
	p := NotifyPayload{
		Trigger: "manual",
		Accounts: []NotifyAccountLine{
			{Provider: "aipy", Label: "甲", Status: "already", Message: "今日已签"},
			{Provider: "dumate", Label: "乙", Status: "already", Message: "今日已签"},
		},
		Summary: "新签 0，已签 2",
	}
	if err := n.send(context.Background(), p, false); err != nil {
		t.Fatalf("全部已签也应推送: %v", err)
	}
	if hit != 1 {
		t.Fatalf("应发出 1 次请求，实际 %d", hit)
	}
}
