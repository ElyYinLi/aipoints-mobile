package panel

import (
	"net/http"
	"strings"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// handleLog 接收前端打的日志（scope=app）。
//
// POST /api/log {"level":"info","scope":"app","msg":"...","round":"..."}
//
// 为什么让前端往引擎塞日志，而不是 console.log 了事：
//   - 手机上根本看不到 console，用户报问题只能靠设置页那一个入口；
//   - 执行逻辑（今天该不该签、判定结果、提示了什么）只有前端知道，
//     这些行必须和 Go 侧的请求日志排在同一条时间线上才排得清因果。
//
// 安全：正文走 maskText 脱敏（与其它日志同一套规则），
// 长度在 maskText 里截到 300 字符，前端不可能灌爆缓冲。
func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Level string `json:"level"`
		Scope string `json:"scope"`
		Msg   string `json:"msg"`
		Round string `json:"round"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	lvl := strings.TrimSpace(in.Level)
	switch lvl {
	case "info", "warn", "error":
	default:
		lvl = "info"
	}
	scope := strings.TrimSpace(in.Scope)
	if scope == "" {
		scope = "app"
	}
	msg := strings.TrimSpace(in.Msg)
	if msg == "" {
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	httpx.AddLog(httpx.LogEntry{
		Level: lvl, Scope: scope, Round: in.Round, Msg: msg,
	})
	writeJSON(w, map[string]any{"ok": true})
}
