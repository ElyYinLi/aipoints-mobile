package httpx

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 日志时间的本地时区偏移。
//
// Go 在 gomobile 进程里读不到系统时区（没有 TZ 环境变量、没有 /etc/localtime），
// time.Now() 一律按 UTC 算 —— 日志里的「7:26」实际是 UTC，比北京时间差 8 小时。
// 推送通知早在 0.6.3 就用「前端上报 getTimezoneOffset」解决了同样的问题，
// 这里照搬：前端每个请求带 X-TZ-Offset header，服务端 middleware 调 SetLogTimezone
// 存下来，AddLog 据此换算。用 atomic 是因为日志由多个 goroutine 并发写。
var logTZOffset int32

// SetLogTimezone 设置日志时间相对 UTC 的偏移（JS getTimezoneOffset 的值，东八区 = -480）。
// 传 0 表示 UTC（或本机就在零时区），此时不做换算。
func SetLogTimezone(offsetMin int) {
	atomic.StoreInt32(&logTZOffset, int32(offsetMin))
}

// logNow 返回按上报时区换算后的当前时间，供日志打时间戳用。
func logNow() time.Time {
	off := atomic.LoadInt32(&logTZOffset)
	if off == 0 {
		return time.Now()
	}
	// JS 的 getTimezoneOffset = UTC - 本地（分钟），东八区是 -480，
	// 所以本地 = UTC - off，FixedZone 要传反号。
	return time.Now().In(time.FixedZone("local", -int(off)*60))
}

// ParseTZHeader 把请求头里的 X-TZ-Offset 解析成分钟数；无效值返回 false。
func ParseTZHeader(v string) (int, bool) {
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < -840 || n > 840 {
		return 0, false
	}
	return n, true
}

// LogEntry 是一条对外可见的运行日志。
//
// 设计约束：日志要贴到聊天里求助，所以字段刻意保持极小，且**绝不记录**
// cookie / authorization / token 这类凭据——只留方法、地址、状态码、耗时、
// 平台、脱敏后的短消息。凭据一律不进日志。
//
// 0.6.4 起对标 aipy-checkin 的细粒度记录（用户原话：「日志给我对标
// aipy-checkin 给搞详细了，细粒度按照你的执行逻辑来」）：
//   - Time 精确到毫秒，同一秒内的多步执行顺序能看出来；
//   - Round 是这一轮一键签到的短 id，能把「一次执行」的十几条串成一条时间线；
//   - Step 是人话步骤名（「签到 百度搭子」「领任务奖励」），
//     比单纯的路由路径更接近用户在界面上看到的东西；
//   - 成功请求也记（info），不再只有出错才有记录。
type LogEntry struct {
	Time     string  `json:"time"`  // HH:MM:SS.mmm
	Date     string  `json:"date"`  // 2006-01-02，跨天时用来分组
	Level    string  `json:"level"` // info / warn / error
	Scope    string  `json:"scope"` // provider id、http、app 等
	Round    string  `json:"round,omitempty"`
	Step     string  `json:"step,omitempty"`
	Method   string  `json:"method,omitempty"`
	Path     string  `json:"path,omitempty"`   // 只留 host 之后的部分，去掉 query
	Status   int     `json:"status,omitempty"` // HTTP 状态码
	CostMS   int64   `json:"cost_ms,omitempty"`
	Msg      string  `json:"msg,omitempty"`
	Code     float64 `json:"code,omitempty"` // 业务码（百度系/HarmonyOS 系都用 code）
	Response string  `json:"resp,omitempty"` // 失败时的响应片段（已脱敏）
}

// logCap 是一次「一键签到」的完整痕迹能留几轮。
//
// 0.6.3 是 400：一轮签到（3 个账号 × 查询+签到+核对+领奖+抽奖）就要 40~60 条，
// 400 只能装 6 轮左右，用户排查时经常发现「关键那轮已经被冲掉了」。
// 1500 条按每条 ~200 字节估算约 300KB 内存，移动端完全承受得起。
const logCap = 1500

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
	now := logNow()
	if e.Time == "" {
		// 毫秒必须留：一轮签到里「查询 → 签到 → 核对」可能全在同一秒内发生，
		// 只到秒就看不出先后，等于把执行逻辑糊成了一团（0.6.4 改）。
		e.Time = now.Format("15:04:05.000")
	}
	if e.Date == "" {
		e.Date = now.Format("2006-01-02")
	}
	// 没显式给 round 的，自动继承当前轮次——见 SetRound 的说明
	if e.Round == "" {
		e.Round = CurrentRound()
	}
	e.Msg = maskText(e.Msg)
	e.Response = maskText(e.Response)
	logMu.Lock()
	if len(logBuf) >= logCap {
		// 简单环形：整体前移一次，均摊 O(1) 摊销（1500 条小结构体拷贝很便宜）
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

// 轮次 id：一轮「一键签到」从开始到结束共用同一个短 id。
//
// 为什么用包级变量而不是逐层传参：签到链路是
// panel.handleCheckin → provider.Checkin → httpx.DoJSON → AddLog，
// 中途还夹着并发查询。要把 round 一路透传下去，得改 provider 接口签名，
// 代价远大于收益。这里用「一个 goroutine 级的约定」：整轮签到在进入时设置，
// 结束时清空，期间该 goroutine 里产生的日志自动带上 round。
//
// 已知取舍：并发查询的多个 goroutine 会共享同一个 round —— 这是**期望行为**，
// 它们本来就属于同一轮。真正的风险是手动单账号签到与整轮签到并发，
// 那种情况下 round 可能混，所以 handleCheckin 里对「单账号」路径不设置 round，
// 只记 step，不误导用户。
var (
	roundMu  sync.Mutex
	curRound string
)

// SetRound 设置当前轮次 id，返回「恢复」函数（defer 调用即可）。
func SetRound(id string) func() {
	roundMu.Lock()
	prev := curRound
	curRound = id
	roundMu.Unlock()
	return func() {
		roundMu.Lock()
		curRound = prev
		roundMu.Unlock()
	}
}

// CurrentRound 返回当前轮次 id（没有则为空串），供需要自己拼日志的调用方取。
func CurrentRound() string {
	roundMu.Lock()
	defer roundMu.Unlock()
	return curRound
}

// 脱敏规则：cookie 串、token 字面量、长 hex/base64 一律替换掉。
// 宁可多盖，也不能把凭据漏进用户随手粘贴的日志里。
var (
	reBearer = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{8,}`)
	// 前缀式：`cookie=xxx` / `cookie: xxx` 直到分隔符。
	//
	// 0.6.4 把 stop 集合加上了空白：原来是 `[^",;\r\n}]*`，会一路吃到空格，
	// 于是「cookie=BDUSS=xxx 出错了」整句被盖成「cookie=***」——
	// 后半句才是真正要看的错误原因，被一起吞掉了。
	// 凭据里不会有空格，遇到空白就停是安全的。
	rePair = regexp.MustCompile(`(?i)\b(cookie|authorization|token|secret|csrftoken|password|passwd|access_token|refresh_token)(\s*[:=]\s*)"?[^",;\s\r\n}]*`)
	// 分号式：Cookie 头里常见 `BDUSS=1; stoken=2; bce-user-info="..."`，
	// 上面那条只盖住第一个键，后面的 stoken/bce-user-info 会漏出来，所以单独按分号切。
	reCookieKv = regexp.MustCompile(`\b(BDUSS|STOKEN|PTOKEN|bce-user-info|bce-session|stoken|ptoken)\s*=\s*"?[^";,\s\r\n]*`)
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
