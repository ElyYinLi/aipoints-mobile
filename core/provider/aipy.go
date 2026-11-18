package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// AiPy Pro（Trustoken 用户中心）适配器。
//
// 接口来源：用户私有仓库 ElyYinLi/aipy-checkin 的 internal/trustoken（已核对源码），
// 与桌面版工具走的是同一套上游接口，因此这里不再靠猜测字段。
//
//	GET  {base}/api/user/self                 当前用户（判定凭据是否有效）
//	GET  {base}/api/user/checkin/status       今日签到状态
//	POST {base}/api/user/checkin              每日签到（无 body）
//	GET  {base}/api/user/daily_tasks/status   每日任务（可选，默认不领）
//
// 鉴权双通道：Cookie session（浏览器登录）或 Authorization: Bearer（控制台令牌）。
// 移动端登录走 WebView，拿到的是 Cookie，所以默认走 Cookie。
//
// 额度口径：接口返回的是整数「额度」，500000 额度 = 1 单位（桌面版 config 的
// quota_per_unit 默认值）。UI 里按单位显示，避免出现一串看不懂的大数。
type AiPy struct {
	c *httpx.Client
}

// NewAiPy 构造适配器。
func NewAiPy(c *httpx.Client) *AiPy { return &AiPy{c: c} }

const (
	aipyDefaultDomain = "www.trustoken.cn"
	// aipyQuotaPerUnit 与官方站点一致：500000 额度 = 1 额度单位。
	aipyQuotaPerUnit = 500000.0
	aipyDefaultUA    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

func (a *AiPy) Meta() Meta {
	return Meta{
		ID: "aipy", Name: "AiPy Pro", Auth: "cookie", Mobile: "ok",
		Note: "Trustoken 用户中心，Cookie 登录后可直接签到",
	}
}

func (a *AiPy) base(c Credential) string {
	d := strings.TrimSpace(c.Domain)
	if d == "" {
		d = aipyDefaultDomain
	}
	d = strings.TrimSuffix(d, "/")
	if hasScheme(d) {
		return d
	}
	return "https://" + d
}

func (a *AiPy) headers(c Credential) map[string]string {
	base := a.base(c)
	h := map[string]string{
		"Accept":           "application/json, text/plain, */*",
		"X-Requested-With": "XMLHttpRequest",
		"Cache-Control":    "no-store",
		"User-Agent":       aipyDefaultUA,
		"Origin":           base,
		"Referer":          base + "/",
	}
	if ck := strings.TrimSpace(c.Cookie); ck != "" {
		h["Cookie"] = ck
	}
	if tok := strings.TrimSpace(c.AccessToken); tok != "" {
		h["Authorization"] = "Bearer " + tok
	}
	return h
}

// Query 读今日签到状态 + 剩余额度。任一子请求失败都只降级不报错。
func (a *AiPy) Query(ctx context.Context, c Credential) (Snapshot, error) {
	snap := Snapshot{Provider: "aipy", AccountID: c.AccountID, At: time.Now()}
	if strings.TrimSpace(c.Cookie) == "" && strings.TrimSpace(c.AccessToken) == "" {
		snap.Error = "缺少 Cookie 或访问令牌"
		return snap, nil
	}
	resp, err := a.c.DoJSON(ctx, "GET", a.base(c)+"/api/user/checkin/status", a.headers(c), nil)
	if err != nil {
		snap.Error = "网络错误: " + err.Error()
		return snap, nil
	}
	if resp.Status == 401 || resp.Status == 403 {
		snap.Error = fmt.Sprintf("登录已失效(%d)，请重新登录", resp.Status)
		return snap, nil
	}
	m, err := resp.JSON()
	if err != nil {
		snap.Error = err.Error()
		return snap, nil
	}
	if !envOK(m) {
		snap.Error = "上游返回: " + envMsg(m)
		return snap, nil
	}
	data := digMap(m, "data")
	if data == nil {
		data = m
	}
	// 字段名以 aipy-checkin/internal/trustoken/types.go 为准，另留几个历史别名。
	snap.CheckedIn = boolOf(pick(data, "checked_in", "checkedIn", "today_checked", "has_checked_in"))
	snap.Streak = int(numOf(pick(data, "continuous_days", "streak", "streak_days")))
	quota := numOf(pick(data, "quota", "remain_quota", "balance", "remaining"))
	if quota > 0 {
		snap.Balance = quota / aipyQuotaPerUnit
		snap.BalanceText = fmtNum(snap.Balance)
	}
	if snap.CheckedIn {
		snap.Detail = "今日已签到"
	} else {
		snap.Detail = "今日未签到"
	}
	// 额度单位为「点数」而非「积分」，UI 按快照里的单位文案显示，避免口径混用。
	snap.Unit = "点数"
	snap.OK = true
	return snap, nil
}

// Checkin 发每日签到。上游用 success 信封表达结果，重复签到时 success=false + 文案。
func (a *AiPy) Checkin(ctx context.Context, c Credential) (Result, error) {
	res := Result{Provider: "aipy", AccountID: c.AccountID}
	if strings.TrimSpace(c.Cookie) == "" && strings.TrimSpace(c.AccessToken) == "" {
		res.Error = "缺少 Cookie 或访问令牌"
		return res, nil
	}
	resp, err := a.c.DoJSON(ctx, "POST", a.base(c)+"/api/user/checkin", a.headers(c), nil)
	if err != nil {
		res.Error = "网络错误: " + err.Error()
		return res, nil
	}
	if resp.Status == 401 || resp.Status == 403 {
		res.Error = fmt.Sprintf("登录已失效(%d)，请重新登录", resp.Status)
		return res, nil
	}
	m, err := resp.JSON()
	if err != nil {
		res.Error = "响应解析失败: " + err.Error()
		return res, nil
	}
	msg := envMsg(m)
	if !envOK(m) {
		// 已签到时上游同样返回 success=false，这里靠文案兜住，避免误报失败。
		lower := strings.ToLower(msg)
		if strings.Contains(msg, "已签") || strings.Contains(msg, "重复") || strings.Contains(lower, "already") {
			res.OK = true
			res.Already = true
			res.Message = msg
			return res, nil
		}
		res.Error = msg
		return res, nil
	}
	data := digMap(m, "data")
	if data == nil {
		data = m
	}
	res.OK = true
	res.Message = "签到成功"
	if msg != "" {
		res.Message = msg
	}
	if v := numOf(pick(data, "reward", "quota", "amount", "gain")); v > 0 {
		// 奖励字段按额度口径换算；小于 1 单位时保留原始值，避免显示成 0。
		if v >= aipyQuotaPerUnit {
			res.Gain = v / aipyQuotaPerUnit
		} else {
			res.Gain = v
		}
	}
	return res, nil
}

// envOK 判断 success 信封。缺 success 字段时退回看 HTTP 语义（已由调用方过滤 4xx）。
func envOK(m map[string]any) bool {
	if v, ok := m["success"]; ok && v != nil {
		return boolOf(v)
	}
	if code, ok := httpx.Num(m["code"]); ok {
		return code == 0 || code == 200
	}
	return true
}

func envMsg(m map[string]any) string {
	s := strOf(pick(m, "message", "msg"))
	if s == "" {
		s = "上游返回失败"
	}
	return s
}

// baseHost 便于测试断言。
func (a *AiPy) baseHost(c Credential) string {
	u, err := url.Parse(a.base(c))
	if err != nil {
		return aipyDefaultDomain
	}
	return u.Host
}

var _ Provider = (*AiPy)(nil)
