package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// newTestPanel 起一个用假存储的面板。
func newTestPanel(t *testing.T, store *fakeStore) *httptest.Server {
	t.Helper()
	s := New(Options{
		Store:    store,
		LoginURL: func(id string) string { return "https://example.com/" + id },
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/providers", s.handleProviders)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/query", s.handleQuery)
	mux.HandleFunc("/api/login/start", s.handleLoginStart)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// fakeStore 是空实现，面板在无账号时也要能正常响应。
type fakeStore struct{}

func (fakeStore) List() ([]store.Account, error)            { return nil, nil }
func (fakeStore) Put(store.Account) error                   { return nil }
func (fakeStore) Delete(providerID, accountID string) error { return nil }

// TestIndexRendersSelfContained 保证面板是可离线运行的单文件：
// 不能有任何外链（CSS/JS/字体/图片），否则离线打开会白屏或样式缺失。
func TestIndexRendersSelfContained(t *testing.T) {
	srv := newTestPanel(t, nil)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 200_000)
	n, _ := resp.Body.Read(buf)
	html := string(buf[:n])

	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("面板应禁缓存，实际 %q", resp.Header.Get("Cache-Control"))
	}
	// 内联图标必须已注塑完成，不能残留占位符
	if strings.Contains(html, "{{ICON:") {
		t.Error("图标占位符未被替换，buildIndex 有遗漏")
	}
	// 禁止外部资源引用
	for _, bad := range []string{"http://", "https://", "//cdn", "@import url("} {
		if strings.Contains(html, bad) {
			t.Errorf("面板不应引用外部资源，发现 %q", bad)
		}
	}
	// 核心交互必须存在
	for _, must := range []string{
		"__pointsAutoCheckin",  // 壳层「打开即签到」入口
		"PointsBridge",         // 原生登录桥
		"points://login",       // scheme 兜底
		"pullToRefresh",        // 下拉刷新
		"prefers-color-scheme", // 暗色适配
		"safe-area-inset",      // 刘海屏适配
	} {
		if !strings.Contains(html, must) {
			t.Errorf("面板缺少关键实现：%s", must)
		}
	}
}

// TestHealth 壳层启动时轮询它。
func TestHealth(t *testing.T) {
	srv := newTestPanel(t, nil)
	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m["ok"] != true {
		t.Fatalf("health 应返回 ok=true，实际 %v", m)
	}
}

// TestLoginStart 面板点「添加账号」时查询登录入口。
func TestLoginStart(t *testing.T) {
	srv := newTestPanel(t, nil)

	resp, err := http.Get(srv.URL + "/api/login/start?provider=trae")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	if m["ok"] != true || m["url"] != "https://example.com/trae" {
		t.Fatalf("应返回 trae 的登录地址，实际 %v", m)
	}

	// 缺参数必须报错而不是 panic
	resp2, err := http.Get(srv.URL + "/api/login/start")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var m2 map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&m2)
	if m2["ok"] != false {
		t.Fatalf("缺 provider 应返回 ok=false，实际 %v", m2)
	}
}

// TestProvidersExposeAll 面板拿到的平台清单要与引擎一致。
func TestProvidersExposeAll(t *testing.T) {
	srv := newTestPanel(t, nil)
	resp, err := http.Get(srv.URL + "/api/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 {
		t.Fatalf("应返回 5 个平台，实际 %d", len(list))
	}
	for _, p := range list {
		if p["id"] == "" || p["name"] == "" {
			t.Errorf("平台元信息缺 id/name: %v", p)
		}
	}
}

// TestLoopbackOnlyRejectsForeignHost 防 DNS rebinding：非回环 Host 一律拒绝。
func TestLoopbackOnlyRejectsForeignHost(t *testing.T) {
	h := loopbackOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	cases := map[string]int{
		"127.0.0.1:7879":          200,
		"localhost:7879":          200,
		"[::1]:7879":              200,
		"evil.com":                403,
		"evil.com:7879":           403,
		"127.0.0.1.evil.com:7879": 403,
	}
	for host, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/query", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host=%s 期望 %d 实际 %d", host, want, rec.Code)
		}
	}
}
