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
//	GET  {base}/api/user/self                 当前用户（余额 User.Quota 在这里）
//	GET  {base}/api/user/checkin/status       今日签到状态（CheckinStatus）
//	POST {base}/api/user/checkin              每日签到（无 body，返回 CheckinResult）
//	GET  {base}/api/user/daily_tasks/status   每日任务（可选，默认不领）
//
// 鉴权双通道：Cookie session（浏览器登录）或 Authorization: Bearer（控制台令牌）。
// 移动端登录走 WebView，拿到的是 Cookie，所以默认走 Cookie。
//
// 单位口径（README + config.example.json 双重确认）：
//
//	quota_per_unit = 500000，stones_per_unit = 100 → 灵石 = 额度 ÷ 5000
const (
	aipyDefaultDomain = "www.trustoken.cn"
	// aipyQuotaPerStone：额度 → 灵石 的除数。官方换算 灵石 = 额度 ÷ 5000。
	aipyQuotaPerStone = 5000.0
	aipyDefaultUA     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// aipySelf 是 /api/user/self 里我们关心的部分（types.go 的 User）。
type aipySelf struct {
	quota    float64
	used     float64
	gift     float64
	ok       bool
	authFail bool
}

type AiPy struct {
	c *httpx.Client
}

// NewAiPy 构造适配器。
func NewAiPy(c *httpx.Client) *AiPy { return &AiPy{c: c} }

func (a *AiPy) Meta() Meta {
	return Meta{
		ID: "aipy", Name: "AiPy Pro", Auth: "cookie", Mobile: "ok",
		Note: "Trustoken 用户中心，Cookie 登录后可直接签到，奖励单位是灵石",
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

// Query 读今日签到状态 + 灵石余额。
func (a *AiPy) Query(ctx context.Context, c Credential) (Snapshot, error) {
	snap := Snapshot{Provider: "aipy", AccountID: c.AccountID, At: time.Now(), Unit: "灵石"}
	if strings.TrimSpace(c.Cookie) == "" && strings.TrimSpace(c.AccessToken) == "" {
		snap.Error = "缺少 Cookie 或访问令牌"
		return snap, nil
	}

	// 余额（/api/user/self）与签到状态互不依赖，并发取：
	// 串行时任一请求慢都会把首屏拖住，用户会以为「卡死」。
	selfCh := make(chan aipySelf, 1)
	go func() { selfCh <- a.fetchSelf(ctx, c) }()

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
		snap.Error = "响应解析失败: " + err.Error()
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

	// 字段名以 types.go 的 CheckinStatus 为准：已签是 checked_in_today。
	// 之前写成 checked_in，上游不认这个键，于是「签完了还是未签到」——用户实测踩过。
	snap.CheckedIn = boolOf(pick(data, "checked_in_today", "checked_in", "checkedIn", "today_checked"))
	snap.Streak = int(numOf(pick(data, "cycle_day", "continuous_days", "streak_days")))

	frozen := boolOf(pick(data, "frozen"))
	canSign := true
	if v, ok := pick(data, "can_sign").(bool); ok {
		canSign = v
	}
	todayReward := numOf(pick(data, "today_total_reward", "today_base_reward"))

	switch {
	case frozen:
		snap.Detail = "签到已冻结，需到官网处理"
	case snap.CheckedIn:
		// 已签：收益只填数值，文案由面板 detailText() 统一拼成
		// 「今日已签 +N 灵石」。之前这里自己拼了「今日已签 · +N 灵石」，
		// 面板又只认 today_gain 字段 → 永远走到「今日已签」分支，+N 被吞掉。
		if todayReward > 0 {
			snap.TodayGain = todayReward / aipyQuotaPerStone
		}
		snap.Detail = "今日已签"
	case !canSign:
		snap.Detail = "今日暂不可签（" + aipyBlockReason(data) + "）"
	case todayReward > 0:
		snap.Detail = "今日可得 " + fmtNum(todayReward/aipyQuotaPerStone) + " 灵石"
	default:
		snap.Detail = "今日未签到"
	}

	// 余额只存在于 /api/user/self 的 User.Quota，checkin/status 没有这个字段。
	// 之前从 status 取 quota，永远是 0 → 表现为「签到后点数毫无变化」。
	if sr := <-selfCh; sr.ok {
		snap.Balance = sr.quota / aipyQuotaPerStone
		snap.BalanceText = fmtNum(snap.Balance)
	}
	snap.OK = true
	return snap, nil
}

// fetchSelf 取 /api/user/self（User.Quota 是额度原值）。
func (a *AiPy) fetchSelf(ctx context.Context, c Credential) aipySelf {
	resp, err := a.c.DoJSON(ctx, "GET", a.base(c)+"/api/user/self", a.headers(c), nil)
	if err != nil {
		return aipySelf{}
	}
	if resp.Status == 401 || resp.Status == 403 {
		return aipySelf{authFail: true}
	}
	m, err := resp.JSON()
	if err != nil || !envOK(m) {
		return aipySelf{}
	}
	data := digMap(m, "data")
	if data == nil {
		data = m
	}
	quota := numOf(pick(data, "quota", "remain_quota", "balance", "remaining"))
	if quota <= 0 {
		return aipySelf{}
	}
	return aipySelf{
		quota: quota,
		used:  numOf(pick(data, "used_quota")),
		gift:  numOf(pick(data, "gift_quota")),
		ok:    true,
	}
}

// aipyBlockReason 把 block_reason（字符串或对象）转成一句人话。
func aipyBlockReason(data map[string]any) string {
	switch v := pick(data, "block_reason").(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	case map[string]any:
		if s := strOf(pick(v, "message", "reason", "msg")); s != "" {
			return s
		}
	}
	return "上游未说明原因"
}

// Checkin 发每日签到。上游用 success 信封表达结果，重复签到时 success=false + 文案。
func (a *AiPy) Checkin(ctx context.Context, c Credential) (Result, error) {
	res := Result{Provider: "aipy", AccountID: c.AccountID, Unit: "灵石"}
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
	// CheckinResult: total_reward / base_reward / milestone_reward，均为额度口径。
	if v := numOf(pick(data, "total_reward", "base_reward", "milestone_reward", "reward", "quota")); v > 0 {
		res.Gain = v / aipyQuotaPerStone
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

// Tasks 返回每日任务列表。上游 data 可能是数组或 {tasks:[...]}，都兼容。
//
// 字段实测（trustoken v1.3.2）：{task_type,title,reward,progress,target,status}，
// status 取 "locked"（未达标）/ "claimed"（已领取）。是否可领只看
// progress>=target 且 status!=claimed，上游不返回 claimed/can_claim 布尔。
func (a *AiPy) Tasks(ctx context.Context, c Credential) ([]Task, error) {
	if strings.TrimSpace(c.Cookie) == "" && strings.TrimSpace(c.AccessToken) == "" {
		return nil, fmt.Errorf("缺少 Cookie 或访问令牌")
	}
	resp, err := a.c.DoJSON(ctx, "GET", a.base(c)+"/api/user/daily_tasks/status", a.headers(c), nil)
	if err != nil {
		return nil, fmt.Errorf("网络错误: %w", err)
	}
	if resp.Status == 401 || resp.Status == 403 {
		return nil, fmt.Errorf("登录已失效(%d)，请重新登录", resp.Status)
	}
	m, err := resp.JSON()
	if err != nil {
		return nil, fmt.Errorf("响应解析失败: %w", err)
	}
	if !envOK(m) {
		return nil, fmt.Errorf("上游返回: %s", envMsg(m))
	}
	data := digMap(m, "data")
	if data == nil {
		data = m
	}
	// 兼容两种形状：顶层数组 / data 是数组 / data.tasks 是数组
	var raws []any
	switch v := pick(data, "tasks", "list").(type) {
	case []any:
		raws = v
	}
	if raws == nil {
		if v, ok := data["__array__"].([]any); ok {
			raws = v
		}
	}
	// data 本身可能是数组（digMap 拿不到数组，退回从 m 取）
	if raws == nil {
		if v, ok := m["data"].([]any); ok {
			raws = v
		}
	}
	if raws == nil {
		if v, ok := m["tasks"].([]any); ok {
			raws = v
		}
	}
	out := make([]Task, 0, len(raws))
	for _, r := range raws {
		mm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		tt := strOf(pick(mm, "task_type", "type", "id"))
		title := strOf(pick(mm, "title", "name", "task_name"))
		if tt == "" && title == "" {
			continue
		}
		reward := numOf(pick(mm, "reward", "bonus", "quota"))
		progress := int(numOf(pick(mm, "progress", "done")))
		target := int(numOf(pick(mm, "target", "need", "total")))
		status := strOf(pick(mm, "status"))
		t := Task{
			TaskType: tt,
			Title:    title,
			// 上游 reward 是额度，换算成灵石与 Query 余额口径一致
			Reward:   reward / aipyQuotaPerStone,
			Progress: progress,
			Target:   target,
			Status:   status,
			Unit:     "灵石",
		}
		// 可领：进度达标 且 未领取
		t.Claimable = target > 0 && progress >= target && status != "claimed"
		out = append(out, t)
	}
	return out, nil
}

// ClaimTask 领取某个每日任务奖励，返回本次奖励（灵石口径，与 Checkin 一致）。
func (a *AiPy) ClaimTask(ctx context.Context, c Credential, taskType string) (Result, error) {
	res := Result{Provider: "aipy", AccountID: c.AccountID, Unit: "灵石"}
	if taskType == "" {
		res.Error = "缺少任务标识"
		return res, nil
	}
	if strings.TrimSpace(c.Cookie) == "" && strings.TrimSpace(c.AccessToken) == "" {
		res.Error = "缺少 Cookie 或访问令牌"
		return res, nil
	}
	resp, err := a.c.DoJSON(ctx, "POST", a.base(c)+"/api/user/daily_tasks/claim",
		a.headers(c), map[string]string{"task_type": taskType})
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
		// 已领取 / 未达标都走 success=false，靠文案兜住
		lower := strings.ToLower(msg)
		if strings.Contains(msg, "已领") || strings.Contains(msg, "重复") ||
			strings.Contains(msg, "未完成") || strings.Contains(msg, "未达标") ||
			strings.Contains(lower, "already") || strings.Contains(lower, "claimed") ||
			strings.Contains(lower, "not completed") {
			res.Error = msg
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
	res.Message = "领取成功"
	if msg != "" {
		res.Message = msg
	}
	// data.reward 是额度口径，换算成灵石
	if v := numOf(pick(data, "reward", "bonus", "quota")); v > 0 {
		res.Gain = v / aipyQuotaPerStone
	}
	return res, nil
}

var _ Provider = (*AiPy)(nil)
var _ TaskClaimer = (*AiPy)(nil)
