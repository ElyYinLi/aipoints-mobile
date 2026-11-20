package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// tokenUpstream 是一个最小的 AiPy 令牌接口假上游，记录收到的请求供断言。
type tokenUpstream struct {
	srv *httptest.Server
	// 记录每个请求的 method + path + body
	got []struct {
		Method string
		Path   string
		Body   map[string]any
	}
	// 列表接口返回的形状："array" | "items" | "top-array"
	listShape string
	// 让 /api/token/page 返回 404，用来验证回退到旧版路径
	breakPage bool
	srv500    bool
}

func newTokenUpstream(t *testing.T) *tokenUpstream {
	t.Helper()
	u := &tokenUpstream{listShape: "items"}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &body)
			}
		}
		u.got = append(u.got, struct {
			Method string
			Path   string
			Body   map[string]any
		}{r.Method, r.URL.RequestURI(), body})

		ok := func(data any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "", "data": data})
		}
		if u.srv500 {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"success":false,"message":"内部错误"}`))
			return
		}

		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/token/page"):
			if u.breakPage {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"success":false,"message":"not found"}`))
				return
			}
			fallthrough
		case r.Method == "GET" && r.URL.Path == "/api/token/":
			tok := map[string]any{
				"id": 1, "name": "默认令牌", "key": "sk-test",
				"status": 1, "remain_quota": 500000, "unlimited_quota": false,
				"expired_time": -1, "created_time": 1700000000,
			}
			switch u.listShape {
			case "array":
				ok([]any{tok})
			case "top-array":
				// 顶层直接是数组：JSON 解到 map 会失败，实现应报错而非 panic
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[{"id":1,"name":"x"}]`))
			default:
				ok(map[string]any{"items": []any{tok}, "total": 1})
			}
		case r.Method == "POST" && r.URL.Path == "/api/token/":
			ok(map[string]any{"id": 7, "name": body["name"], "key": "sk-created"})
		case r.Method == "PUT" && r.URL.Path == "/api/token/":
			ok(nil)
		case r.Method == "PUT" && r.URL.Path == "/api/token/":
			ok(nil)
		case r.Method == "DELETE":
			ok(nil)
		default:
			ok(nil)
		}
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *tokenUpstream) provider(t *testing.T) *AiPy {
	t.Helper()
	return NewAiPy(httpx.New())
}

func (u *tokenUpstream) cred() Credential {
	return Credential{AccountID: "a1", Cookie: "session=x", Domain: u.srv.URL}
}

func TestListTokensShapes(t *testing.T) {
	for _, shape := range []string{"items", "array"} {
		u := newTokenUpstream(t)
		u.listShape = shape
		list, err := u.provider(t).ListTokens(context.Background(), u.cred())
		if err != nil {
			t.Fatalf("%s: 列表失败: %v", shape, err)
		}
		if len(list) != 1 || list[0].Key != "sk-test" || list[0].Name != "默认令牌" {
			t.Fatalf("%s: 解析结果不对: %+v", shape, list)
		}
		if !list[0].NeverExpires() || !list[0].Enabled() {
			t.Fatalf("%s: 状态/过期语义不对: %+v", shape, list[0])
		}
	}
}

// 新版分页接口 404 时必须回退到旧版路径，而不是直接报错。
func TestListTokensFallsBackToLegacyPath(t *testing.T) {
	u := newTokenUpstream(t)
	u.breakPage = true
	list, err := u.provider(t).ListTokens(context.Background(), u.cred())
	if err != nil {
		t.Fatalf("应回退成功: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("回退后应拿到 1 条: %+v", list)
	}
	var sawLegacy bool
	for _, g := range u.got {
		if strings.Contains(g.Path, "p=0") {
			sawLegacy = true
		}
	}
	if !sawLegacy {
		t.Fatal("没有请求旧版路径 /api/token/?p=0")
	}
}

func TestListTokens5xxIsRetryable(t *testing.T) {
	u := newTokenUpstream(t)
	u.srv500 = true
	_, err := u.provider(t).ListTokens(context.Background(), u.cred())
	if err == nil {
		t.Fatal("5xx 应返回错误")
	}
	ae := AsAPIError(err)
	if ae.Kind != ErrServer {
		t.Fatalf("5xx 应归为 server，实际 %q", ae.Kind)
	}
	if !ae.IsRetryable() {
		t.Fatal("server 错误应可重试")
	}
}

// 缺凭据时不应发请求。
func TestTokenRequiresCredential(t *testing.T) {
	u := newTokenUpstream(t)
	_, err := u.provider(t).ListTokens(context.Background(), Credential{AccountID: "a1", Domain: u.srv.URL})
	if err == nil || AsAPIError(err).Kind != ErrAuth {
		t.Fatalf("缺凭据应报 auth: %v", err)
	}
	if len(u.got) != 0 {
		t.Fatalf("缺凭据不应发请求，实际发了 %d 次", len(u.got))
	}
}

// expired_time=0 必须补成 -1，否则上游会建一条立刻过期的令牌。
func TestCreateTokenDefaultsNeverExpire(t *testing.T) {
	u := newTokenUpstream(t)
	tok, err := u.provider(t).CreateToken(context.Background(), u.cred(),
		TokenInput{Name: "新令牌", ExpiredTime: 0})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if tok.Key != "sk-created" {
		t.Fatalf("应返回上游给的 key: %+v", tok)
	}
	var sent map[string]any
	for _, g := range u.got {
		if g.Method == "POST" {
			sent = g.Body
		}
	}
	if sent == nil {
		t.Fatal("没有发出 POST /api/token/")
	}
	if v, _ := sent["expired_time"].(float64); v != -1 {
		t.Fatalf("expired_time 应补成 -1，实际 %v", sent["expired_time"])
	}
}

func TestCreateTokenRequiresName(t *testing.T) {
	u := newTokenUpstream(t)
	if _, err := u.provider(t).CreateToken(context.Background(), u.cred(), TokenInput{}); err == nil {
		t.Fatal("空名称应报错")
	}
	if len(u.got) != 0 {
		t.Fatal("空名称不应发请求")
	}
}

// 删除必须带尾部斜杠，否则上游 404。
func TestDeleteTokenURLHasTrailingSlash(t *testing.T) {
	u := newTokenUpstream(t)
	if err := u.provider(t).DeleteToken(context.Background(), u.cred(), 42); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	var path string
	for _, g := range u.got {
		if g.Method == "DELETE" {
			path = g.Path
		}
	}
	if path != "/api/token/42/" {
		t.Fatalf("删除路径应为 /api/token/42/，实际 %q", path)
	}
}

// 启停必须走 status_only=true 的 query 参数，且 body 只带 id 与 status。
func TestSetTokenStatusUsesStatusOnly(t *testing.T) {
	u := newTokenUpstream(t)
	if err := u.provider(t).SetTokenStatus(context.Background(), u.cred(), 3, 2); err != nil {
		t.Fatalf("改状态失败: %v", err)
	}
	var path string
	var body map[string]any
	for _, g := range u.got {
		if g.Method == "PUT" {
			path, body = g.Path, g.Body
		}
	}
	if !strings.Contains(path, "status_only=true") {
		t.Fatalf("路径应含 status_only=true，实际 %q", path)
	}
	if len(body) != 2 {
		t.Fatalf("body 应只有 id 与 status，实际 %v", body)
	}
	if v, _ := body["status"].(float64); v != 2 {
		t.Fatalf("status 应为 2，实际 %v", body["status"])
	}
}

func TestSetTokenStatusRejectsBadArgs(t *testing.T) {
	u := newTokenUpstream(t)
	p := u.provider(t)
	if err := p.SetTokenStatus(context.Background(), u.cred(), 0, 1); err == nil {
		t.Fatal("id=0 应报错")
	}
	if err := p.SetTokenStatus(context.Background(), u.cred(), 1, 5); err == nil {
		t.Fatal("status=5 应报错")
	}
	if len(u.got) != 0 {
		t.Fatal("非法参数不应发请求")
	}
}

// 毫秒时间戳要被归一成秒，否则前端会显示成 5 万年后。
func TestParseTokenNormalizesMillis(t *testing.T) {
	tok := parseToken(map[string]any{
		"id": 1, "expired_time": float64(1893456000000), // 2030-01-01 毫秒
	})
	if tok.ExpiredTime != 1893456000 {
		t.Fatalf("毫秒应转秒，实际 %d", tok.ExpiredTime)
	}
}

// 上游不给 status 时按启用处理，否则界面全是「禁用」很误导。
func TestParseTokenDefaultsEnabled(t *testing.T) {
	if got := parseToken(map[string]any{"id": 1}); got.Status != 1 {
		t.Fatalf("缺 status 应按启用处理，实际 %d", got.Status)
	}
}
