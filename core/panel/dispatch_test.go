package panel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
	"github.com/ElyYinLi/aipoints-mobile/core/provider"
)

// 0.6.9 方案 A2：iOS 用 pointsapi:// scheme 在进程内分发请求，不经 TCP。
// 这组断言确保 Dispatch 复用了同一套 handler（路由、GET/POST、返回 JSON 都正确），
// 是「去掉本机 HTTP server 后页面仍能读数据」的核心保障。
func TestDispatchRoutesToExistingHandlers(t *testing.T) {
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New()), Version: "test"})

	// GET /api/providers：走 handleProviders，返回数组 JSON
	code, body := s.Dispatch("GET", "/api/providers", "", "")
	if code != 200 {
		t.Fatalf("providers 状态码 = %d, body=%s", code, body)
	}
	var ps []any
	if err := json.Unmarshal([]byte(body), &ps); err != nil {
		t.Fatalf("providers 不是 JSON 数组: %v, body=%s", err, body)
	}

	// GET /api/health：返回 {"ok":true}
	code, body = s.Dispatch("GET", "/api/health", "", "")
	if code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("health 异常: code=%d body=%s", code, body)
	}

	// GET 主文档：返回单文件 HTML（含面板标志），证明 scheme 可加载整个 UI
	code, body = s.Dispatch("GET", "/", "", "")
	if code != 200 || !strings.Contains(body, "<html") && !strings.Contains(body, "<!doctype") {
		t.Fatalf("主文档未通过 Dispatch 返回: code=%d len=%d", code, len(body))
	}
}

func TestDispatchPOSTCarriesBody(t *testing.T) {
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New())})

	// POST /api/log：写一条前端日志，body 必须被 handler 收到（证明内存 request 的 body 没丢）
	payload := `{"scope":"app","text":"dispatch-probe-0.6.9"}`
	code, body := s.Dispatch("POST", "/api/log", payload, "-480")
	if code != 200 {
		t.Fatalf("POST /api/log 状态码 = %d, body=%s", code, body)
	}
	if !strings.Contains(body, `"ok"`) {
		t.Fatalf("POST /api/log 返回异常: %s", body)
	}
}

func TestDispatchUnknownRouteFallsToIndex(t *testing.T) {
	s := New(Options{Store: fakeStore{}, Reg: provider.NewRegistry(httpx.New())})
	// 未注册的 path 落到 handleIndex（SPA 行为），至少不是 500
	code, _ := s.Dispatch("GET", "/some/unknown/path", "", "")
	if code != 200 {
		t.Fatalf("未知路由应回退到面板首页，实际 %d", code)
	}
}
