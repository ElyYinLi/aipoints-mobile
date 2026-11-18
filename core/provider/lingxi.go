package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// Lingxi 金山 WPS 灵犀适配器（Cookie 型，最容易移植到移动端）。
//
// 接口来源：AI积分助手 v2.2.0 静态逆向。
//
//	GET  /api/public/v1/tasks                                  任务与签到状态
//	POST /api/public/v1/tasks/daily_check_in/claim?date=Y-M-D  领取每日签到
//	GET  /api/public/v1/credits/balance                        智点余额
//
// 规则全部由接口给：tasks 里 type=daily 那项的 extra.check_in_days 是本周期每天状态，
// extra.today_day 指出今天第几天，status=available 才可领、claimed 表示已领。
// 本地绝不写死奖励数值。
type Lingxi struct {
	c *httpx.Client
}

// NewLingxi 构造适配器。
func NewLingxi(c *httpx.Client) *Lingxi { return &Lingxi{c: c} }

const lingxiBase = "https://lingxi.kdocs.cn"

func (l *Lingxi) Meta() Meta {
	return Meta{
		ID: "lingxi", Name: "灵犀", Auth: "cookie", Mobile: "ok",
		Note: "纯 cookie + REST；WebView 登录 kdocs 后即可直连",
	}
}

func (l *Lingxi) base(c Credential) string {
	d := strings.TrimSpace(c.Domain)
	if d == "" {
		return lingxiBase
	}
	d = strings.TrimSuffix(d, "/")
	if strings.Contains(d, "://") {
		return d
	}
	return "https://" + d
}

func (l *Lingxi) headers(c Credential) map[string]string {
	return map[string]string{
		"Cookie":     c.Cookie,
		"Origin":     l.base(c),
		"Referer":    l.base(c) + "/",
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	}
}

// dailyTask 是 tasks 里我们关心的那一项。
type dailyTask struct {
	Status      string
	TodayDay    int
	CheckInDays []any
	Raw         map[string]any
}

// pickDaily 从 tasks 响应里挑出 type=daily 的任务。
func pickDaily(m map[string]any) (*dailyTask, bool) {
	candidates := []any{}
	// 兼容三种可能形状：data.tasks / data.list / tasks
	for _, path := range [][]string{{"data", "tasks"}, {"data", "list"}, {"tasks"}} {
		if s := httpx.PickSlice(m, path...); s != nil {
			candidates = s
			break
		}
	}
	if candidates == nil {
		if d, ok := httpx.Pick(m, "data").([]any); ok {
			candidates = d
		}
	}
	for _, it := range candidates {
		t, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if httpx.Str(t["type"]) != "daily" {
			continue
		}
		dt := &dailyTask{Status: httpx.Str(t["status"]), Raw: t}
		if extra, ok := t["extra"].(map[string]any); ok {
			dt.TodayDay = int(numOr(extra["today_day"], 0))
			if s, ok := extra["check_in_days"].([]any); ok {
				dt.CheckInDays = s
			}
		}
		return dt, true
	}
	return nil, false
}

// Query 读今日状态与智点余额。
func (l *Lingxi) Query(ctx context.Context, c Credential) (Snapshot, error) {
	snap := Snapshot{Provider: "lingxi", AccountID: c.AccountID, At: time.Now()}
	if c.Cookie == "" {
		snap.Error = "缺少 cookie"
		return snap, nil
	}
	resp, err := l.c.DoJSON(ctx, "GET", l.base(c)+"/api/public/v1/tasks", l.headers(c), nil)
	if err != nil {
		snap.Error = "网络错误: " + err.Error()
		return snap, nil
	}
	// 状态码先于 JSON 解析：401/403 的响应体不是 JSON
	if resp.Status == 401 || resp.Status == 403 {
		snap.Error = fmt.Sprintf("登录已失效(%d)，请重新登录灵犀", resp.Status)
		return snap, nil
	}
	m, err := resp.JSON()
	if err != nil {
		snap.Error = "响应解析失败: " + err.Error()
		return snap, nil
	}
	dt, found := pickDaily(m)
	if found {
		snap.CheckedIn = dt.Status == "claimed"
		snap.Streak = dt.TodayDay
		switch dt.Status {
		case "available":
			snap.Detail = "今日可领"
		case "claimed":
			snap.Detail = "今日已领"
		default:
			snap.Detail = "状态: " + dt.Status
		}
	}
	if bal, ok := l.queryBalance(ctx, c); ok {
		snap.Balance = bal
		snap.BalanceText = fmtNum(bal)
		snap.Unit = "智点"
	}
	snap.OK = true
	return snap, nil
}

func (l *Lingxi) queryBalance(ctx context.Context, c Credential) (float64, bool) {
	resp, err := l.c.DoJSON(ctx, "GET", l.base(c)+"/api/public/v1/credits/balance", l.headers(c), nil)
	if err != nil {
		return 0, false
	}
	m, err := resp.JSON()
	if err != nil {
		return 0, false
	}
	for _, path := range [][]string{{"data", "balance"}, {"data", "credits"}, {"data", "total"}, {"balance"}} {
		if v, ok := httpx.Num(httpx.Pick(m, path...)); ok {
			return v, true
		}
	}
	return 0, false
}

// Checkin 领今日签到。必须先查状态，available 才发写请求（幂等靠服务端，但我们不做无意义写）。
func (l *Lingxi) Checkin(ctx context.Context, c Credential) (Result, error) {
	res := Result{Provider: "lingxi", AccountID: c.AccountID}
	if c.Cookie == "" {
		res.Error = "缺少 cookie"
		return res, nil
	}
	// 先查状态，避免重复写
	snap, _ := l.Query(ctx, c)
	if snap.CheckedIn {
		res.OK = true
		res.Already = true
		res.Message = "今日已签到"
		return res, nil
	}
	date := time.Now().Format("2006-01-02")
	u := l.base(c) + "/api/public/v1/tasks/daily_check_in/claim?date=" + url.QueryEscape(date)
	resp, err := l.c.DoJSON(ctx, "POST", u, l.headers(c), map[string]any{})
	if err != nil {
		res.Error = "网络错误: " + err.Error()
		return res, nil
	}
	m, err := resp.JSON()
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	msgL := strOf(pick(m, "msg", "message"))
	if code, ok := httpx.Num(m["code"]); ok && code != 0 {
		// 已经领过时上游以业务码报错形式返回，按已签处理，不报错
		if strings.Contains(msgL, "已领") || strings.Contains(msgL, "已签") || strings.Contains(msgL, "重复") {
			res.OK, res.Already, res.Message = true, true, msgL
			return res, nil
		}
		res.Error = fmt.Sprintf("业务码 %s: %s", fmtNum(code), msgL)
		return res, nil
	}
	res.OK = true
	res.Message = "签到成功"
	if v, ok := httpx.Num(httpx.Pick(m, "data", "credits")); ok {
		res.Gain = v
	}
	return res, nil
}

var _ Provider = (*Lingxi)(nil)
