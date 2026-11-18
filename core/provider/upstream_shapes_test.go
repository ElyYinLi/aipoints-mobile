package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// 本文件锁住「上游响应形状与业务码」的处理。
// 这些分支都是照第三方实测实现（L0NE-6/Trae-AutoCheckin、Minatoxiaohu/agent-auto-signin）
// 与桌面端行为对齐的，改坏了会让用户看到「签到失败」但实际上已经签上了。

func stub(t *testing.T, handler http.HandlerFunc) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	return srv.URL, srv.Close
}

// TestTraeRateLimited 9074 是排队限流，属常态，不能报成「签到失败」吓用户。
func TestTraeRateLimited(t *testing.T) {
	url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"checked_in": false}})
		default:
			writeJSON(w, map[string]any{"code": 9074, "message": "参与用户过多"})
		}
	})
	defer done()

	p := NewTrae(httpx.New())
	res, _ := p.Checkin(context.Background(), Credential{AccountID: "t", AccessToken: "j", Domain: url})
	if res.OK {
		t.Fatal("限流不应算成功")
	}
	if res.Error != "" {
		t.Fatalf("限流不应报错：%q", res.Error)
	}
	if !strings.Contains(res.Message, "稍后") {
		t.Fatalf("限流应提示稍后再试，实际 %q", res.Message)
	}
}

// TestTraeAlreadyByCode 9004/9095 是「今日已签」，必须落到 Already。
func TestTraeAlreadyByCode(t *testing.T) {
	for _, code := range []int{9004, 9095} {
		url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/status") {
				writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"checked_in": false}})
				return
			}
			writeJSON(w, map[string]any{"code": code})
		})
		p := NewTrae(httpx.New())
		res, _ := p.Checkin(context.Background(), Credential{AccountID: "t", AccessToken: "j", Domain: url})
		if !res.OK || !res.Already {
			t.Fatalf("code=%d 应判为已签，实际 %+v", code, res)
		}
		done()
	}
}

// TestTraeAuthCode 业务码 1001 要给出「重新登录」，而不是泛泛的失败。
func TestTraeAuthCode(t *testing.T) {
	url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 1001, "message": "unauthenticated"})
	})
	defer done()

	p := NewTrae(httpx.New())
	snap, _ := p.Query(context.Background(), Credential{AccountID: "t", AccessToken: "j", Domain: url})
	if snap.OK || !strings.Contains(snap.Error, "重新登录") {
		t.Fatalf("1001 应提示重新登录，实际 %+v", snap)
	}
}

// TestWorkBuddyEnvelopeVariants 上游信封有裸对象和 data 包裹两种形状，都要能读。
func TestWorkBuddyEnvelopeVariants(t *testing.T) {
	cases := []map[string]any{
		{"today_checked_in": true, "total_credits": 1200, "streak_days": 4},
		{"data": map[string]any{"today_checked_in": true, "total_credits": 1200, "streak_days": 4}},
		{"result": map[string]any{"today_checked_in": true, "total_credits": 1200, "streak_days": 4}},
	}
	for i, body := range cases {
		url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "checkin-activity-status") {
				writeJSON(w, body)
				return
			}
			http.NotFound(w, r) // 余额接口失败，不影响结论
		})
		p := NewWorkBuddy(httpx.New())
		snap, _ := p.Query(context.Background(), Credential{AccountID: "w", AccessToken: "t", Domain: url})
		if !snap.OK || !snap.CheckedIn || snap.Streak != 4 {
			t.Fatalf("第 %d 种信封解析失败: %+v", i+1, snap)
		}
		done()
	}
}

// TestWorkBuddyInactive 活动未开启时不能当成未签到去发写请求。
func TestWorkBuddyInactive(t *testing.T) {
	url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"active": false, "activity_name": "每日签到"})
	})
	defer done()

	p := NewWorkBuddy(httpx.New())
	snap, _ := p.Query(context.Background(), Credential{AccountID: "w", AccessToken: "t", Domain: url})
	if !snap.OK || snap.CheckedIn {
		t.Fatalf("活动未开启不应判为已签: %+v", snap)
	}
	if !strings.Contains(snap.Detail, "未开启") {
		t.Fatalf("应说明活动未开启，实际 %q", snap.Detail)
	}
}

// TestWorkBuddyAlreadyByMessage 服务端用文案表达「已签」时也要认。
func TestWorkBuddyAlreadyByMessage(t *testing.T) {
	url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 500, "msg": "今日已签到，请勿重复"})
	})
	defer done()

	p := NewWorkBuddy(httpx.New())
	res, _ := p.Checkin(context.Background(), Credential{AccountID: "w", AccessToken: "t", Domain: url})
	if !res.OK || !res.Already {
		t.Fatalf("文案含「已签」应判为已签: %+v", res)
	}
}

// TestDigAnyEnvelope 验证信封下钻不会误命中无关字段。
func TestDigAnyEnvelope(t *testing.T) {
	m := map[string]any{
		"other": 1,
		"data": map[string]any{
			"nested": map[string]any{"target": "found"},
		},
	}
	if got := digAny(m, "target"); got != "found" {
		t.Fatalf("应能下钻到 target，实际 %v", got)
	}
	if got := digAny(m, "missing"); got != nil {
		t.Fatalf("不存在的字段应为 nil，实际 %v", got)
	}
	if got := digAny(m, "nested"); got == nil {
		t.Fatal("应能取到中间层对象")
	}
}

// TestAiPyQuotaUnit AiPy 的额度口径是 500000=1，换算错了显示会差五个数量级。
func TestAiPyQuotaUnit(t *testing.T) {
	url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"success": true, "data": map[string]any{"checked_in": false, "quota": 2500000}})
	})
	defer done()

	p := NewAiPy(httpx.New())
	snap, _ := p.Query(context.Background(), Credential{AccountID: "a", Cookie: "x=1", Domain: url})
	if snap.Balance != 5 {
		t.Fatalf("2500000 额度应为 5 单位，实际 %v", snap.Balance)
	}
	if snap.Unit != "点数" {
		t.Fatalf("单位应为点数，实际 %q", snap.Unit)
	}
}

// TestJSONBodyShape Trae 的请求体必须带 req_source（缺了被拒）。
func TestJSONBodyShape(t *testing.T) {
	var got map[string]any
	url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"checked_in": true}})
	})
	defer done()

	p := NewTrae(httpx.New())
	_, _ = p.Query(context.Background(), Credential{AccountID: "t", AccessToken: "j", Domain: url})
	if v, ok := got["req_source"].(float64); !ok || v != 1 {
		t.Fatalf("请求体应含 req_source=1，实际 %v", got)
	}
}

// TestAuthStatusBeatsParseError 401/403 的响应体常是 HTML 或空，
// 若先解析 JSON 就会把「登录失效」误报成「响应解析失败」——用户看到的是错误的原因。
func TestAuthStatusBeatsParseError(t *testing.T) {
	htmlBody := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(401)
		_, _ = w.Write([]byte("<html>401 Unauthorized</html>"))
	}
	cases := map[string]func(url string) string{
		"workbuddy": func(u string) string {
			s, _ := NewWorkBuddy(httpx.New()).Query(context.Background(), Credential{AccountID: "x", AccessToken: "t", Domain: u})
			return s.Error
		},
		"dumate": func(u string) string {
			s, _ := NewDumate(httpx.New()).Query(context.Background(), Credential{AccountID: "x", Cookie: "a=1", Domain: u})
			return s.Error
		},
		"lingxi": func(u string) string {
			s, _ := NewLingxi(httpx.New()).Query(context.Background(), Credential{AccountID: "x", Cookie: "a=1", Domain: u})
			return s.Error
		},
		"trae": func(u string) string {
			s, _ := NewTrae(httpx.New()).Query(context.Background(), Credential{AccountID: "x", AccessToken: "t", Domain: u})
			return s.Error
		},
		"aipy": func(u string) string {
			s, _ := NewAiPy(httpx.New()).Query(context.Background(), Credential{AccountID: "x", Cookie: "a=1", Domain: u})
			return s.Error
		},
	}
	for name, run := range cases {
		url, done := stub(t, htmlBody)
		msg := run(url)
		done()
		if strings.Contains(msg, "解析") {
			t.Errorf("%s：401 不应报解析失败，实际 %q", name, msg)
		}
		if !strings.Contains(msg, "失效") && !strings.Contains(msg, "鉴权失败") {
			t.Errorf("%s：应提示登录失效，实际 %q", name, msg)
		}
	}
}
