package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// TestWorkBuddyQueryAndCheckin 用假服务器验证请求形状（方法/路径/身份头）与响应解析。
// 价值：把接口契约固化下来，官方接口一变测试先红。
func TestWorkBuddyQueryAndCheckin(t *testing.T) {
	// 加锁：WorkBuddy 现在并发发起「状态」与「余额」两个请求，
	// handler 会被多个 goroutine 同时进入，无锁写 map 会直接 panic。
	var mu sync.Mutex
	var seen = map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen["path"] = r.URL.Path
		seen["method"] = r.Method
		seen["auth"] = r.Header.Get("Authorization")
		seen["uid"] = r.Header.Get("X-User-Id")
		seen["origin"] = r.Header.Get("Origin")
		seen["ua"] = r.Header.Get("User-Agent")
		mu.Unlock()
		switch r.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"checked_in": true, "streak": 7, "total_credits": 300,
			}})
		case "/billing/meter/get-user-resource-summary":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"Packages": []any{
					map[string]any{"CycleRemainCapacity": 1200.5},
					map[string]any{"CycleRemainCapacity": 300.0},
				},
			}})
		case "/v2/billing/meter/daily-checkin":
			writeJSON(w, map[string]any{"code": 10001, "msg": "今日已签到"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewWorkBuddy(httpx.New())
	cred := Credential{AccountID: "a1", AccessToken: "tok", UID: "u1", EnterpriseID: "e1", Domain: srv.URL}

	snap, err := p.Query(context.Background(), cred)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.OK || !snap.CheckedIn {
		t.Fatalf("期望已签到，实际 %+v", snap)
	}
	if snap.Streak != 7 {
		t.Fatalf("streak 期望 7，实际 %d", snap.Streak)
	}
	// 余额接口（无 /v2）必须被调用并求和
	if snap.Balance != 1500.5 {
		t.Fatalf("余额期望 1500.5，实际 %v", snap.Balance)
	}
	if seen["auth"] != "Bearer tok" {
		t.Fatalf("Authorization 头错误: %q", seen["auth"])
	}
	if seen["uid"] != "u1" {
		t.Fatalf("X-User-Id 头错误: %q", seen["uid"])
	}
	if !strings.Contains(seen["ua"], "Mozilla") {
		t.Fatal("必须带浏览器 User-Agent（否则余额接口 403）")
	}

	res, err := p.Checkin(context.Background(), cred)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Already {
		t.Fatalf("业务码 10001 应判定为已签到，实际 %+v", res)
	}
}

// TestLingxiFlow 验证 tasks 解析 + claim 路径带 date 参数。
func TestLingxiFlow(t *testing.T) {
	var claimPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/public/v1/tasks":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"tasks": []any{
				map[string]any{"type": "weekly", "status": "available"},
				map[string]any{"type": "daily", "status": "available",
					"extra": map[string]any{"today_day": 3}},
			}}})
		case r.URL.Path == "/api/public/v1/credits/balance":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"balance": 5800}})
		case strings.HasPrefix(r.URL.Path, "/api/public/v1/tasks/daily_check_in/claim"):
			claimPath = r.URL.String()
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"credits": 100}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewLingxi(httpx.New())
	cred := Credential{AccountID: "k1", Cookie: "SID=1", Domain: srv.URL}

	snap, _ := p.Query(context.Background(), cred)
	if snap.CheckedIn {
		t.Fatal("status=available 时不应判定为已签")
	}
	if snap.Balance != 5800 {
		t.Fatalf("智点余额期望 5800，实际 %v", snap.Balance)
	}
	if snap.Streak != 3 {
		t.Fatalf("today_day 期望 3，实际 %d", snap.Streak)
	}

	res, _ := p.Checkin(context.Background(), cred)
	if !res.OK {
		t.Fatalf("签到应成功: %+v", res)
	}
	if !strings.Contains(claimPath, "date=") {
		t.Fatalf("claim 必须带 date 参数，实际 %q", claimPath)
	}
}

// TestDumateQuota 验证「可用 = 总额 - 已用」与写接口带 x-bce-request-id。
func TestDumateQuota(t *testing.T) {
	var gotReqID, gotCSRF string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dumate/points/loginBonusInfo":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"signed_today": false}})
		case "/api/dumate/points/quota_overview":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"total_points": 2200, "used_points": 500,
			}})
		case "/api/dumate/points/loginBonus":
			gotReqID = r.Header.Get("x-bce-request-id")
			gotCSRF = r.Header.Get("csrftoken")
			writeJSON(w, map[string]any{"code": 0, "msg": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewDumate(httpx.New())
	// bce-user-info 里的值即 csrftoken，写接口必须原样带上（去掉首尾引号）
	cred := Credential{
		AccountID: "d1",
		Cookie:    `BDUSS=1; bce-user-info="csrf-abc-123"; stoken=2`,
		Domain:    srv.URL,
	}

	snap, _ := p.Query(context.Background(), cred)
	if snap.Balance != 1700 {
		t.Fatalf("可用积分期望 1700，实际 %v", snap.Balance)
	}
	res, _ := p.Checkin(context.Background(), cred)
	if !res.OK {
		t.Fatalf("签到应成功: %+v", res)
	}
	if len(gotReqID) < 16 {
		t.Fatalf("写接口必须带 x-bce-request-id，实际 %q", gotReqID)
	}
	if gotCSRF != "csrf-abc-123" {
		t.Fatalf("csrftoken 头应从 bce-user-info 取值并去引号，实际 %q", gotCSRF)
	}
}

// TestRegistryHasCore 确保核心平台都注册成功。
func TestRegistryHasCore(t *testing.T) {
	reg := NewRegistry(httpx.New())
	// 已接入的五家。加平台时这里要一起改——故意让它红，避免忘了写新平台的测试。
	want := []string{"workbuddy", "lingxi", "dumate", "trae", "aipy"}
	for _, id := range want {
		if _, ok := reg.Get(id); !ok {
			t.Fatalf("缺少平台 %s", id)
		}
	}
	if got := len(reg.All()); got != len(want) {
		t.Fatalf("期望 %d 个平台，实际 %d", len(want), got)
	}
}

// TestMissingCredentialFailsFast 缺凭据要立刻返回可读错误，不能真发请求。
func TestMissingCredentialFailsFast(t *testing.T) {
	p := NewLingxi(httpx.New())
	snap, err := p.Query(context.Background(), Credential{})
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if snap.Error == "" {
		t.Fatal("缺 cookie 时应有错误说明")
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// TestTraeFlow 验证 Trae 的认证头、状态解析与「已签不重复领取」。
func TestTraeFlow(t *testing.T) {
	var seenAuth, seenUA, seenRegion string
	claimed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenUA = r.Header.Get("User-Agent")
		seenRegion = r.Header.Get("X-User-Region")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["req_source"] == nil {
			t.Errorf("请求体缺 req_source，服务端会拒成 9074")
		}
		switch r.URL.Path {
		case "/trae/api/v2/ug/checkin_credits/status":
			// 第一次查：没签；领过之后再查：已签
			writeJSON(w, map[string]any{"data": map[string]any{
				"checked_in": claimed > 0, "credits": 300, "extra_credits": 50,
			}})
		case "/trae/api/v2/ug/checkin_credits/claim":
			claimed++
			writeJSON(w, map[string]any{"code": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewTrae(httpx.New())
	cred := Credential{AccountID: "t1", AccessToken: "jwt-xyz", Domain: srv.URL}

	snap, _ := p.Query(context.Background(), cred)
	if snap.CheckedIn {
		t.Fatalf("初始状态应为未签到")
	}
	// credits 与 extra_credits 是两种口径（本次可领 / 额外赠送），相加会把奖励算大，
	// 这里取 credits 优先；对照 L0NE-6/Trae-AutoCheckin 的 reward_credits() 实现。
	if snap.Balance != 300 {
		t.Fatalf("积分应取 credits=300，实际 %v", snap.Balance)
	}
	if seenAuth != "Cloud-IDE-JWT jwt-xyz" {
		t.Fatalf("Authorization 头格式错误: %q", seenAuth)
	}
	if !strings.Contains(seenUA, "TRAE SOLO CN") {
		t.Fatalf("UA 应为 Trae 客户端形态，实际 %q", seenUA)
	}
	if seenRegion != "CN" {
		t.Fatalf("X-User-Region 应为 CN，实际 %q", seenRegion)
	}

	res, _ := p.Checkin(context.Background(), cred)
	if !res.OK || res.Already {
		t.Fatalf("首次签到应成功且非已签: %+v", res)
	}
	res2, _ := p.Checkin(context.Background(), cred)
	if !res2.Already {
		t.Fatalf("第二次签到应判定为已签，实际 %+v", res2)
	}
	if claimed != 1 {
		t.Fatalf("claim 接口只应被调用 1 次，实际 %d", claimed)
	}
}

// TestWorkBuddyBalanceFallback 验证余额接口多路径兜底：
// 主路径 404 时应自动尝试下一个候选。
func TestWorkBuddyBalanceFallback(t *testing.T) {
	var tried []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tried = append(tried, r.URL.Path)
		if r.URL.Path == "/billing/meter/get-user-resource-summary" {
			http.NotFound(w, r) // 模拟该路径已下线
			return
		}
		writeJSON(w, map[string]any{
			"code": 0,
			"data": map[string]any{"Packages": []any{
				map[string]any{"CycleRemainCapacity": 800},
			}},
		})
	}))
	defer srv.Close()

	wb := NewWorkBuddy(httpx.New())
	sum, ok := wb.queryBalance(context.Background(), Credential{AccountID: "b1", AccessToken: "t", Domain: srv.URL})
	if !ok || sum != 800 {
		t.Fatalf("兜底后应拿到 800，实际 ok=%v sum=%v", ok, sum)
	}
	if len(tried) < 2 {
		t.Fatalf("应至少尝试两个路径，实际 %v", tried)
	}
}
