package panel

import (
	"net/http"
	"strconv"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// RecentLogs 导出给 gomobile 壳层：面板没起来时也能从原生侧取日志排查。
func RecentLogs(limit int) []httpx.LogEntry { return httpx.Logs(limit) }

// handleLogs 返回最近的运行日志。
//
//	GET /api/logs?limit=200   → {"logs":[{"time","level","scope","method","path","status","cost_ms","msg"}]}
//
// 只读、无副作用。日志内容在写入 httpx 时已脱敏（cookie/token/长 hex 全部替换成 ***），
// 所以整份日志可以放心让用户长按复制发出去求助。
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	// 说明：日志写入侧已按条数环形裁剪，这里只做切片，不额外落盘
	logs := httpx.Logs(limit)
	// 倒序给前端：最新的一条在最上面，用户第一眼就能看到刚发生的问题
	out := make([]httpx.LogEntry, 0, len(logs))
	for i := len(logs) - 1; i >= 0; i-- {
		out = append(out, logs[i])
	}
	writeJSON(w, map[string]any{"ok": true, "logs": out, "count": len(out)})
}

// handleLogsClear 清空日志。
//
//	POST /api/logs/clear
func (s *Server) handleLogsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	httpx.ClearLogs()
	writeJSON(w, map[string]any{"ok": true})
}
