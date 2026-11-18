package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
	"github.com/ElyYinLi/aipoints-mobile/core/provider"
)

func newOAuthServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := New(Options{
		Store: &memStore{},
		Reg:   provider.NewRegistry(httpx.New()),
		LoginURL: func(id string) string {
			if id == "dumate" {
				return "https://dumate.baidu.com/"
			}
			return ""
		},
	})
	ts := httptest.NewServer(s.srv.Handler)
	t.Cleanup(ts.Close)
	return ts
}

// TestLoginRedirectNonOAuth 非 OAuth 平台应 302 到真实业务站。
// 百度搭子之前被写成了文心一言的域名，页面上根本没有登录入口。
func TestLoginRedirectNonOAuth(t *testing.T) {
	ts := newOAuthServer(t)
	cli := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := cli.Get(ts.URL + "/login/dumate")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("应 302，实际 %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "https://dumate.baidu.com/" {
		t.Fatalf("跳转目标不符: %q", loc)
	}
}

// TestOAuthFinishWithoutPending 没有发起过登录时不能假装成功。
func TestOAuthFinishWithoutPending(t *testing.T) {
	ts := newOAuthServer(t)
	resp, err := http.Post(ts.URL+"/api/oauth/finish", "application/json",
		strings.NewReader(`{"provider":"workbuddy"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	msg, _ := out["error"].(string)
	if out["ok"] != false || !strings.Contains(msg, "没有进行中") {
		t.Fatalf("应提示没有进行中的登录: %v", out)
	}
}

// TestOAuthStartRejectsNonTokenPlatform 只有 token 型平台需要 OAuth。
func TestOAuthStartRejectsNonTokenPlatform(t *testing.T) {
	ts := newOAuthServer(t)
	resp, err := http.Post(ts.URL+"/api/oauth/start", "application/json",
		strings.NewReader(`{"provider":"aipy"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != false {
		t.Fatalf("非 token 平台不应启动 OAuth: %v", out)
	}
}
