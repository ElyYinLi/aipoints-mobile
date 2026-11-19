package httpx

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// LogEntry 是一条对外可见的运行日志。
//
// 设计约束：日志要贴到聊天里求助，所以字段刻意保持极小，且**绝不记录**
// cookie / authorization / token 这类凭据——只留方法、地址、状态码、耗时、
// 平台、脱敏后的短消息。凭据一律不进日志。
type LogEntry struct {
	Time     string  `json:"time"`  // HH:MM:SS
	Date     string  `json:"date"`  // 2006-01-02，跨天时用来分组
	Level    string  `json:"level"` // info / warn / error
	Scope    string  `json:"scope"` // provider id、http、app 等
	Method   string  `json:"method,omitempty"`
	Path     string  `json:"path,omitempty"`   // 只留 host 之后的部分，去掉 query
	Status   int     `json:"status,omitempty"` // HTTP 状态码
	CostMS   int64   `json:"cost_ms,omitempty"`
	Msg      string  `json:"msg,omitempty"`
	Code     float64 `json:"code,omitempty"` // 业务码（百度系/HarmonyOS 系都用 code）
	Response string  `json:"resp,omitempty"` // 失败时的响应片段（已脱敏）
}

const logCap = 400

var (
	logMu   sync.Mutex
	logBuf  = make([]LogEntry, 0, logCap)
	logHook func(LogEntry)
)

// SetLogHook 注册一个旁路回调（壳层可用来落盘）。传 nil 取消。
func SetLogHook(fn func(LogEntry)) {
	logMu.Lock()
	logHook = fn
	logMu.Unlock()
}

// AddLog 追加一条日志。并发安全，超出容量丢最旧的。
func AddLog(e LogEntry) {
	if e.Level == "" {
		e.Level = "info"
	}
	now := time.Now()
	if e.Time == "" {
		e.Time = now.Format("15:04:05")
	}
	if e.Date == "" {
		e.Date = now.Format("2006-01-02")
	}
	e.Msg = maskText(e.Msg)
	e.Response = maskText(e.Response)
	logMu.Lock()
	if len(logBuf) >= logCap {
		// 简单环形：整体前移一次，均摊 O(1) 摊销（400 条小结构体拷贝很便宜）
		copy(logBuf, logBuf[len(logBuf)-logCap+1:])
		logBuf = logBuf[:logCap-1]
	}
	logBuf = append(logBuf, e)
	fn := logHook
	logMu.Unlock()
	if fn != nil {
		fn(e)
	}
}

// Logs 返回最近的日志（按时间正序，最新的在最后）。limit<=0 返回全部。
func Logs(limit int) []LogEntry {
	logMu.Lock()
	defer logMu.Unlock()
	n := len(logBuf)
	if limit > 0 && limit < n {
		return append([]LogEntry(nil), logBuf[n-limit:]...)
	}
	return append([]LogEntry(nil), logBuf...)
}

// ClearLogs 清空日志缓冲。
func ClearLogs() {
	logMu.Lock()
	logBuf = logBuf[:0]
	logMu.Unlock()
}

// 脱敏规则：cookie 串、token 字面量、长 hex/base64 一律替换掉。
// 宁可多盖，也不能把凭据漏进用户随手粘贴的日志里。
var (
	reBearer = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{8,}`)
	// 前缀式：`cookie=xxx` / `cookie: xxx` 直到分隔符
	rePair = regexp.MustCompile(`(?i)\b(cookie|authorization|token|secret|csrftoken|password|passwd|access_token|refresh_token)(\s*[:=]\s*)"?[^",;\r\n}]*`)
	// 分号式：Cookie 头里常见 `BDUSS=1; stoken=2; bce-user-info="..."`，
	// 上面那条只盖住第一个键，后面的 stoken/bce-user-info 会漏出来，所以单独按分号切。
	reCookieKv = regexp.MustCompile(`\b(BDUSS|STOKEN|PTOKEN|bce-user-info|bce-session|stoken|ptoken)\s*=\s*"?[^";,\r\n]*`)
	reLongHex  = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
)

func maskText(s string) string {
	if s == "" {
		return s
	}
	s = reBearer.ReplaceAllString(s, "Bearer ***")
	s = rePair.ReplaceAllString(s, "$1=***")
	s = reCookieKv.ReplaceAllString(s, "$1=***")
	s = reLongHex.ReplaceAllString(s, "***")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// LogPath 把完整 URL 压成 host 之后的路径，去掉 query 与 userinfo。
func LogPath(rawURL string) string {
	u := rawURL
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	host := u
	rest := ""
	if i := strings.IndexByte(u, '/'); i >= 0 {
		host, rest = u[:i], u[i:]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.IndexByte(host, '@'); i >= 0 {
		host = host[i+1:]
	}
	return host + rest
}
