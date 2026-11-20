package provider

import (
	"fmt"
	"strings"
)

// ErrKind 把上游失败归类，决定「要不要重试」和「给用户看什么」。
//
// 为什么需要它：各平台过去一律 fmt.Errorf 拼字符串，panel 只能把整句塞进
// Result.Error。结果是断网、凭证过期、命中风控三种完全不同的处境在界面上
// 长得一模一样，用户只能盲试。分类之后前端可以按 Kind 换颜色和文案，
// 引擎也能据此决定是否退避重试。
type ErrKind string

const (
	ErrNone      ErrKind = ""           // 没有错误
	ErrAuth      ErrKind = "auth"       // 未登录 / 凭证失效，需重新登录
	ErrAlready   ErrKind = "already"    // 今日已签到（幂等，视为成功）
	ErrCaptcha   ErrKind = "captcha"    // 需要安全验证，程序不绕过
	ErrRateLimit ErrKind = "rate_limit" // 瞬时限流，值得重试
	ErrBlocked   ErrKind = "blocked"    // 风控冻结 / 任务门槛 / 不在时间窗
	ErrNetwork   ErrKind = "network"    // 传输层：DNS、连接、超时
	ErrServer    ErrKind = "server"     // 5xx
	ErrUnknown   ErrKind = "unknown"
)

// APIError 是带分类的上游错误。Body 保留原始响应片段，方便面板「查看原始响应」
// 诊断上游改版——各平台解析失败时只报「字段缺失」是查不出问题的。
type APIError struct {
	Kind    ErrKind
	Status  int
	Message string
	Body    string
	Path    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Status > 0 {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	return "未知错误"
}

// IsRetryable 判断这个错误值不值得自动重试。
//
// 只认「换个时刻再试可能成功」的三类：网络抖动、服务端临时故障、瞬时限流。
// 认证失败、已签到、验证码、风控冻结重试多少次都一样，重试反而更像在攻击上游。
func (e *APIError) IsRetryable() bool {
	switch e.Kind {
	case ErrNetwork, ErrServer, ErrRateLimit:
		return true
	default:
		return false
	}
}

// AsAPIError 把任意 error 归一成 *APIError。非分类错误归为 ErrUnknown，
// 保留原 Message，不丢信息。
func AsAPIError(err error) *APIError {
	if err == nil {
		return nil
	}
	if ae, ok := err.(*APIError); ok {
		return ae
	}
	return &APIError{Kind: ErrUnknown, Message: err.Error()}
}

// classifyWords 是关键词表：按优先级顺序匹配，先命中先返回。
//
// 顺序有讲究：auth 必须在 blocked 之前（「无权进行此操作」包含「操作」，
// 而 blocked 里有「不可领取」），already 必须在 blocked 之前
// （「今日已签」不能落进风控分支）。这条顺序是照 aipy-checkin 线上验证过的
// 结果确认的，调整前请先看 provider/errkind_test.go 里的用例。
var classifyWords = []struct {
	kind  ErrKind
	words []string
}{
	{ErrAuth, []string{
		"未登录", "登录已过期", "登录失效", "未提供 access token", "无权进行此操作",
		"unauthorized", "invalid token", "token 已过期", "session expired",
		"会话已失效", "请重新登录", "credential",
	}},
	{ErrAlready, []string{
		"已签到", "今日已签", "重复签到", "already checked", "already signed", "已领取",
	}},
	{ErrCaptcha, []string{
		"验证码", "安全验证", "人机验证", "captcha", "turnstile", "滑块",
	}},
	{ErrRateLimit, []string{
		"频繁", "稍后再试", "人数过多", "too many", "rate limit", "try again later", "限流",
	}},
	{ErrBlocked, []string{
		"冻结", "任务门槛", "不可领取", "不在签到时间", "签到时间", "已被管理员限制",
		"未完成", "blocked", "风控", "禁止",
	}},
}

// Classify 按 HTTP 状态码 + 响应体判定错误类别。
//
// 先看正文（上游习惯把真实原因写在 message 里，状态码常年 200），
// 再退回状态码。正文匹配用 ToLower 做大小写无关比对。
func Classify(status int, body string) ErrKind {
	lower := strings.ToLower(body)
	for _, group := range classifyWords {
		for _, w := range group.words {
			if strings.Contains(lower, strings.ToLower(w)) {
				return group.kind
			}
		}
	}
	switch {
	case status >= 500:
		return ErrServer
	case status == 401 || status == 403:
		return ErrAuth
	case status == 429:
		return ErrRateLimit
	case status >= 400:
		return ErrUnknown
	}
	return ErrNone
}

// ClassifyResponse 是 Classify 的便捷包装：从一次 httpx.Response 直接得出错误。
//
// 约定：2xx 且信封 success 为真时返回 nil；否则返回带分类的 *APIError。
// 各平台信封字段名不完全一致（code/status/success），由调用方先用 envOK
// 判定业务成败，这里只负责「失败时怎么分类」。
func ClassifyResponse(status int, body []byte, msg string, path string) *APIError {
	ae := &APIError{
		Kind:    Classify(status, string(body)),
		Status:  status,
		Message: msg,
		Path:    path,
	}
	if ae.Message == "" {
		ae.Message = fmt.Sprintf("HTTP %d", status)
	}
	if len(body) > 800 {
		ae.Body = string(body[:800]) + "..."
	} else {
		ae.Body = string(body)
	}
	return ae
}
