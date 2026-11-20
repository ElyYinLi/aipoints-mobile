package provider

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// TestWorkBuddyOAuthFlow 锁住 OAuth 三步：state → token（含 11217 等待）→ account。
//
// 这条链路是用户实测「网页登录拿不到 access token」的正解：
// WorkBuddy 的 token 只能由服务端签发，网页登录不会把它写进 cookie。
func TestWorkBuddyOAuthFlow(t *testing.T) {
	var polls int32
	url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/v2/plugin/auth/state"):
			if got := r.Header.Get("X-No-Authorization"); got != "true" {
				t.Errorf("auth/state 必须带 X-No-Authorization: true，实际 %q", got)
			}
			if r.Method != http.MethodPost {
				t.Errorf("auth/state 应为 POST，实际 %s", r.Method)
			}
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"state": "st-1", "authUrl": "https://example.com/oauth?state=st-1",
			}})
		case strings.HasSuffix(r.URL.Path, "/v2/plugin/auth/token"):
			if r.URL.Query().Get("state") != "st-1" {
				t.Errorf("轮询必须带 state，实际 %q", r.URL.RawQuery)
			}
			if atomic.AddInt32(&polls, 1) == 1 {
				writeJSON(w, map[string]any{"code": 11217, "msg": "login ing..."})
				return
			}
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"access_token": "tok-abc", "refresh_token": "ref-def", "expires_in": 3600,
			}})
		case strings.HasSuffix(r.URL.Path, "/v2/plugin/login/account"):
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-abc") {
				t.Errorf("account 必须带 Bearer token，实际 %q", r.Header.Get("Authorization"))
			}
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"uid": "u1", "enterprise_id": "e1", "tenant_id": "t1", "nickname": "小明",
			}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	defer done()

	wb := NewWorkBuddy(httpx.New())
	ctx := context.Background()
	cred := Credential{Domain: url}

	st, err := wb.OAuthStart(ctx, cred)
	if err != nil || st.State != "st-1" || !strings.Contains(st.AuthURL, "state=st-1") {
		t.Fatalf("start 不符: %+v err=%v", st, err)
	}

	tok, waiting, err := wb.OAuthPoll(ctx, cred, st.State)
	if err != nil || !waiting {
		t.Fatalf("首次轮询应等待: tok=%+v waiting=%v err=%v", tok, waiting, err)
	}

	tok, waiting, err = wb.OAuthPoll(ctx, cred, st.State)
	if err != nil || waiting || tok.AccessToken != "tok-abc" || tok.RefreshToken != "ref-def" {
		t.Fatalf("第二次轮询应拿到 token: %+v waiting=%v err=%v", tok, waiting, err)
	}

	acc, err := wb.OAuthAccount(ctx, cred, st.State, tok.AccessToken)
	if err != nil || acc.UID != "u1" || acc.EnterpriseID != "e1" || acc.Nickname != "小明" {
		t.Fatalf("account 不符: %+v err=%v", acc, err)
	}
}

// TestAiPyNotCheckedInDetail 未签到时应显示「今日可得 N 灵石」，而不是一片空白。
func TestAiPyNotCheckedInDetail(t *testing.T) {
	url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/user/checkin/status"):
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{
				"checked_in_today": false, "cycle_day": 3, "today_base_reward": 20000, "can_sign": true,
			}})
		case strings.HasSuffix(r.URL.Path, "/api/user/self"):
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota": 500000}})
		}
	})
	defer done()

	p := NewAiPy(httpx.New())
	snap, _ := p.Query(context.Background(), Credential{Cookie: "x=1", Domain: url})
	if snap.CheckedIn {
		t.Fatal("应为未签到")
	}
	// 未签也走统一口径：数值放 TodayGain，文案由面板 detailText() 拼成
	// 「今日未签 · 可得 +N 灵石」。provider 不再自己拼带单位的字符串。
	if snap.TodayGain != 4 {
		t.Fatalf("未签时 today_gain 应为 4 灵石，实际 %v（Detail=%q）", snap.TodayGain, snap.Detail)
	}
	if snap.Detail != "今日未签到" {
		t.Fatalf("未签状态文案应为「今日未签到」，实际 %q", snap.Detail)
	}
	if snap.Balance != 100 {
		t.Fatalf("余额应为 100 灵石，实际 %v", snap.Balance)
	}
}

// TestAiPyFrozen 冻结账号不能当成「未签到」去引导用户点签到。
func TestAiPyFrozen(t *testing.T) {
	url, done := stub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/user/checkin/status"):
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{
				"checked_in_today": false, "frozen": true,
			}})
		case strings.HasSuffix(r.URL.Path, "/api/user/self"):
			writeJSON(w, map[string]any{"success": true, "data": map[string]any{"quota": 1000}})
		}
	})
	defer done()

	p := NewAiPy(httpx.New())
	snap, _ := p.Query(context.Background(), Credential{Cookie: "x=1", Domain: url})
	if !strings.Contains(snap.Detail, "冻结") {
		t.Fatalf("冻结应明确提示，实际 %q", snap.Detail)
	}
}
