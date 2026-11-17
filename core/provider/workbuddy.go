package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// WorkBuddy（CodeBuddy）适配器。
//
// 接口来源：对 AI积分助手 v2.2.0 的静态逆向（见 spec/api-map.json）。
//
//	POST {base}/v2/billing/meter/checkin-activity-status   签到状态 / 累计积分
//	POST {base}/v2/billing/meter/daily-checkin             每日签到（body {}）
//	POST {base}/billing/meter/get-user-resource-summary    余额（注意：无 /v2 前缀）
//
// 关键约束（逆向结论，勿改）：
//   - 余额接口必须带 Origin / Referer / User-Agent 三件套，否则 403。
//   - 身份头 X-User-Id / X-Enterprise-Id / X-Tenant-Id 缺失时部分账号返回空数据。
//   - 本平台**不需要**设备指纹（Turing Shield 只用于成长计划部分接口）。
type WorkBuddy struct {
	c *httpx.Client
}

// NewWorkBuddy 构造适配器。
func NewWorkBuddy(c *httpx.Client) *WorkBuddy { return &WorkBuddy{c: c} }

const workbuddyDefaultDomain = "www.workbuddy.cn"

func (w *WorkBuddy) Meta() Meta {
	return Meta{
		ID: "workbuddy", Name: "WorkBuddy", Auth: "token", Mobile: "ok",
		Note: "官方浏览器 OAuth 可直出 token，移动端可完整实现",
	}
}

// base 返回完整站点前缀。Domain 里带 scheme 时原样使用（便于本地抓包/测试）。
func (w *WorkBuddy) base(c Credential) string {
	d := strings.TrimSpace(c.Domain)
	if d == "" {
		d = workbuddyDefaultDomain
	}
	d = strings.TrimSuffix(d, "/")
	if strings.Contains(d, "://") {
		return d
	}
	return "https://" + d
}

func (w *WorkBuddy) host(c Credential) string {
	u, err := url.Parse(w.base(c))
	if err != nil || u.Host == "" {
		return workbuddyDefaultDomain
	}
	return u.Host
}

// headers 组装请求头。刻意不打印任何值：token 不允许出现在日志里。
func (w *WorkBuddy) headers(c Credential) map[string]string {
	h := w.host(c)
	return map[string]string{
		"Authorization":   "Bearer " + c.AccessToken,
		"Origin":          "https://" + h,
		"Referer":         "https://" + h + "/",
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"X-User-Id":       c.UID,
		"X-Enterprise-Id": c.EnterpriseID,
		"X-Tenant-Id":     c.TenantID,
	}
}

// Query 读签到状态与累计积分。余额走另一个接口，失败不影响 CheckedIn 结论。
func (w *WorkBuddy) Query(ctx context.Context, c Credential) (Snapshot, error) {
	snap := Snapshot{Provider: "workbuddy", AccountID: c.AccountID, At: time.Now()}
	if c.AccessToken == "" {
		snap.Error = "缺少 accessToken"
		return snap, nil
	}
	resp, err := w.c.DoJSON(ctx, "POST", w.base(c)+"/v2/billing/meter/checkin-activity-status", w.headers(c), map[string]any{})
	if err != nil {
		snap.Error = "网络错误: " + err.Error()
		return snap, nil
	}
	m, err := resp.JSON()
	if err != nil {
		snap.Error = err.Error()
		return snap, nil
	}
	if resp.Status == 401 || resp.Status == 403 {
		snap.Error = fmt.Sprintf("鉴权失败(%d)：token 过期或身份头不匹配", resp.Status)
		return snap, nil
	}

	// 字段路径为推断值（逆向只拿到接口形状，未见真实响应）：
	// 优先取 data 层，取不到再退回顶层，任一命中即用。
	data, _ := httpx.Pick(m, "data").(map[string]any)
	if data == nil {
		data = m
	}
	snap.CheckedIn = httpx.Bool(firstOf(data, "checked_in", "checkedIn", "today_checked", "has_checked_in"))
	snap.Streak = int(numOr(firstOf(data, "streak", "continuous_days", "streak_days"), 0))
	if v, ok := httpx.Num(firstOf(data, "total_credits", "credits", "total", "amount")); ok {
		snap.Balance = v
	}
	snap.BalanceText = httpx.Str(firstOf(data, "total_credits", "credits", "total"))
	snap.Detail = "签到状态接口返回正常"

	// 余额口径（App 首屏），失败只降级不报错
	if bal, ok := w.queryBalance(ctx, c); ok {
		snap.Balance = bal
		snap.BalanceText = fmt.Sprintf("%.2f", bal)
	}
	snap.OK = true
	return snap, nil
}

// queryBalance 取 App 首屏余额，data.Packages[].CycleRemainCapacity 求和。
func (w *WorkBuddy) queryBalance(ctx context.Context, c Credential) (float64, bool) {
	resp, err := w.c.DoJSON(ctx, "POST", w.base(c)+"/billing/meter/get-user-resource-summary", w.headers(c), map[string]any{})
	if err != nil {
		return 0, false
	}
	m, err := resp.JSON()
	if err != nil {
		return 0, false
	}
	pkgs := httpx.PickSlice(m, "data", "Packages")
	var sum float64
	found := false
	for _, p := range pkgs {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := httpx.Num(pm["CycleRemainCapacity"]); ok {
			sum += v
			found = true
		}
	}
	return sum, found
}

// Checkin 发每日签到。已签时接口回业务码 10001（HTTP 仍是 200）。
func (w *WorkBuddy) Checkin(ctx context.Context, c Credential) (Result, error) {
	res := Result{Provider: "workbuddy", AccountID: c.AccountID}
	if c.AccessToken == "" {
		res.Error = "缺少 accessToken"
		return res, nil
	}
	resp, err := w.c.DoJSON(ctx, "POST", w.base(c)+"/v2/billing/meter/daily-checkin", w.headers(c), map[string]any{})
	if err != nil {
		res.Error = "网络错误: " + err.Error()
		return res, nil
	}
	m, err := resp.JSON()
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	code, _ := httpx.Num(m["code"])
	msg := httpx.Str(m["msg"])
	if code == 10001 {
		res.OK = true
		res.Already = true
		res.Message = "今日已签到"
		return res, nil
	}
	if code != 0 && code != 200 {
		res.Error = fmt.Sprintf("业务码 %v: %s", code, msg)
		return res, nil
	}
	res.OK = true
	res.Message = "签到成功"
	if data, ok := m["data"].(map[string]any); ok {
		if v, ok := httpx.Num(firstOf(data, "gain", "credits", "amount", "reward")); ok {
			res.Gain = v
		}
	}
	return res, nil
}

// --- 包内小工具（各 provider 共用）---

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func numOr(v any, def float64) float64 {
	if f, ok := httpx.Num(v); ok {
		return f
	}
	return def
}

var _ Provider = (*WorkBuddy)(nil)
