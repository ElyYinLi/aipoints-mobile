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
		// 未签但平台已告知今日奖励：填数值，文案交给 detailText() 统一拼成
		// 「今日未签 · 可得 +N 灵石」，与已签状态保持同一口径。
		snap.TodayGain = todayReward / aipyQuotaPerStone
		snap.Detail = "今日未签到"
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
var _ TokenManager = (*AiPy)(nil)

// ---------- API 令牌（密钥）管理 ----------
//
// 上游接口（与 aipy-checkin/trustoken/client.go 核对一致）：
//
//	GET    /api/token/page?p=1&size=100    分页列表（新版）
//	GET    /api/token/?p=0&size=100        列表（旧版，新版失败时回退）
//	POST   /api/token/                     新建
//	PUT    /api/token/                     编辑（整对象回填）
//	DELETE /api/token/{id}/                删除（注意尾部斜杠）
//	PUT    /api/token/?status_only=true    仅改启停，body {id, status}
//
// 这套接口形状比较怪（列表两种形状、删除带尾斜杠、启停走 query 参数），
// 都是线上验证过的原样照抄，不要「顺手规范化」，否则上游 404。

// aipyTokenErr 把一次令牌请求的失败统一转成 *APIError（带分类与原始响应）。
func aipyTokenErr(resp *httpx.Response, msg, path string) *APIError {
	body := []byte(nil)
	status := 0
	if resp != nil {
		body, status = resp.Body, resp.Status
	}
	if msg == "" && resp != nil {
		if m, err := resp.JSON(); err == nil {
			msg = envMsg(m)
		}
	}
	return ClassifyResponse(status, body, msg, path)
}

// tokenCredOK 令牌接口既支持 Cookie 也支持 Bearer；两者都空时直接失败，
// 避免发一个必然 401 的请求还把上游计一次异常。
func tokenCredOK(c Credential) bool {
	return strings.TrimSpace(c.Cookie) != "" || strings.TrimSpace(c.AccessToken) != ""
}

// parseToken 把一个上游令牌对象解析成 APIToken。
//
// 字段级容错：上游对同一含义用过 quota / remain_quota 两种键，
// 过期时间可能是秒也可能是毫秒（>= 1e12 视为毫秒）。宁可不显示也不能显示错。
func parseToken(m map[string]any) APIToken {
	t := APIToken{
		ID:           int64(numOf(pick(m, "id"))),
		Name:         strOf(pick(m, "name")),
		Key:          strOf(pick(m, "key", "token")),
		Status:       int(numOf(pick(m, "status"))),
		RemainQuota:  numOf(pick(m, "remain_quota", "quota")),
		UsedQuota:    numOf(pick(m, "used_quota")),
		Unlimited:    boolOf(pick(m, "unlimited_quota")),
		ModelLimits:  strOf(pick(m, "model_limits")),
		AllowIPs:     strOf(pick(m, "allow_ips")),
		CreatedTime:  int64(numOf(pick(m, "created_time"))),
		AccessedTime: int64(numOf(pick(m, "accessed_time"))),
		ExpiredTime:  int64(numOf(pick(m, "expired_time"))),
	}
	if t.ExpiredTime >= 1e12 { // 毫秒时间戳
		t.ExpiredTime /= 1000
	}
	if t.Status == 0 {
		// 上游没给 status 时按「启用」处理，避免界面上全部显示禁用
		t.Status = 1
	}
	return t
}

// ListTokens 列出当前账号的全部令牌。
//
// 上游 data 有两种形状：直接数组，或 {items:[], total:N}。先试新版分页接口，
// 失败再退旧版——两个都失败才报错。
func (a *AiPy) ListTokens(ctx context.Context, c Credential) ([]APIToken, error) {
	if !tokenCredOK(c) {
		return nil, &APIError{Kind: ErrAuth, Message: "缺少 Cookie 或访问令牌"}
	}
	paths := []string{
		"/api/token/page?p=1&size=100",
		"/api/token/?p=0&size=100",
	}
	var lastErr error
	for _, p := range paths {
		resp, err := a.c.DoJSON(ctx, "GET", a.base(c)+p, a.headers(c), nil)
		if err != nil {
			lastErr = &APIError{Kind: ErrNetwork, Message: "网络错误: " + err.Error(), Path: p}
			continue
		}
		if resp.Status == 401 || resp.Status == 403 {
			return nil, aipyTokenErr(resp, "登录已失效，请重新登录", p)
		}
		m, jerr := resp.JSON()
		if jerr != nil {
			lastErr = aipyTokenErr(resp, "", p)
			continue
		}
		if !envOK(m) {
			lastErr = aipyTokenErr(resp, envMsg(m), p)
			continue
		}
		return parseTokenList(m), nil
	}
	if lastErr == nil {
		lastErr = &APIError{Kind: ErrUnknown, Message: "无法获取令牌列表"}
	}
	return nil, lastErr
}

// parseTokenList 从信封里抠出令牌数组，兼容 []、{items:[]}、{data:[]} 三种形状。
// 抽成独立函数便于单测（不需要起 HTTP 服务）。
func parseTokenList(m map[string]any) []APIToken {
	out := make([]APIToken, 0, 4)
	var arr []any
	switch data := m["data"].(type) {
	case []any:
		arr = data
	case map[string]any:
		if v, ok := data["items"].([]any); ok {
			arr = v
		} else if v, ok := data["list"].([]any); ok {
			arr = v
		}
	case nil:
		// 有的版本直接返回顶层数组，JSON 解到 map 会失败，已在调用方兜住
	}
	if arr == nil {
		if v, ok := m["items"].([]any); ok {
			arr = v
		}
	}
	for _, item := range arr {
		if mm, ok := item.(map[string]any); ok {
			out = append(out, parseToken(mm))
		}
	}
	return out
}

// CreateToken 新建令牌，返回含完整 key 的对象。
func (a *AiPy) CreateToken(ctx context.Context, c Credential, in TokenInput) (APIToken, error) {
	if !tokenCredOK(c) {
		return APIToken{}, &APIError{Kind: ErrAuth, Message: "缺少 Cookie 或访问令牌"}
	}
	if strings.TrimSpace(in.Name) == "" {
		return APIToken{}, &APIError{Kind: ErrUnknown, Message: "令牌名称不能为空"}
	}
	body := tokenBody(in)
	resp, err := a.c.DoJSON(ctx, "POST", a.base(c)+"/api/token/", a.headers(c), body)
	if err != nil {
		return APIToken{}, &APIError{Kind: ErrNetwork, Message: "网络错误: " + err.Error()}
	}
	if resp.Status == 401 || resp.Status == 403 {
		return APIToken{}, aipyTokenErr(resp, "登录已失效，请重新登录", "/api/token/")
	}
	m, jerr := resp.JSON()
	if jerr != nil {
		return APIToken{}, aipyTokenErr(resp, "", "/api/token/")
	}
	if !envOK(m) {
		return APIToken{}, aipyTokenErr(resp, envMsg(m), "/api/token/")
	}
	// 创建响应里带完整 key；data 可能是对象，也可能直接平铺在顶层
	if data, ok := m["data"].(map[string]any); ok {
		t := parseToken(data)
		if t.Name == "" {
			t.Name = in.Name
		}
		return t, nil
	}
	t := parseToken(m)
	if t.Name == "" {
		t.Name = in.Name
	}
	if t.Key == "" {
		// 上游只回 success 不回对象：不当作失败（令牌已建成），
		// 让前端提示用户「已创建，请刷新列表查看」
		t.Key = ""
	}
	return t, nil
}

// tokenBody 把 TokenInput 转成上游请求体。
//
// ExpiredTime == 0 必须补成 -1：上游把 0 当作「已过期」，直接传会建出一条
// 立刻失效的令牌，用户会以为程序建错了。这条是 aipy-checkin 踩过的坑。
func tokenBody(in TokenInput) map[string]any {
	exp := in.ExpiredTime
	if exp == 0 {
		exp = -1
	}
	return map[string]any{
		"name":                 strings.TrimSpace(in.Name),
		"remain_quota":         in.RemainQuota,
		"expired_time":         exp,
		"unlimited_quota":      in.Unlimited,
		"model_limits":         in.ModelLimits,
		"model_limits_enabled": strings.TrimSpace(in.ModelLimits) != "",
		"allow_ips":            in.AllowIPs,
	}
}

// UpdateToken 编辑令牌（整对象回填，上游不接受局部更新）。
func (a *AiPy) UpdateToken(ctx context.Context, c Credential, in TokenInput) error {
	if !tokenCredOK(c) {
		return &APIError{Kind: ErrAuth, Message: "缺少 Cookie 或访问令牌"}
	}
	if in.ID <= 0 {
		return &APIError{Kind: ErrUnknown, Message: "缺少令牌 ID"}
	}
	body := tokenBody(in)
	body["id"] = in.ID
	resp, err := a.c.DoJSON(ctx, "PUT", a.base(c)+"/api/token/", a.headers(c), body)
	if err != nil {
		return &APIError{Kind: ErrNetwork, Message: "网络错误: " + err.Error()}
	}
	return aipyTokenOK(resp, "/api/token/")
}

// DeleteToken 删除令牌。不可撤销。
func (a *AiPy) DeleteToken(ctx context.Context, c Credential, id int64) error {
	if !tokenCredOK(c) {
		return &APIError{Kind: ErrAuth, Message: "缺少 Cookie 或访问令牌"}
	}
	if id <= 0 {
		return &APIError{Kind: ErrUnknown, Message: "缺少令牌 ID"}
	}
	// 尾部斜杠不能少，否则上游 404
	p := fmt.Sprintf("/api/token/%d/", id)
	resp, err := a.c.DoJSON(ctx, "DELETE", a.base(c)+p, a.headers(c), nil)
	if err != nil {
		return &APIError{Kind: ErrNetwork, Message: "网络错误: " + err.Error()}
	}
	return aipyTokenOK(resp, p)
}

// SetTokenStatus 启用（1）/ 禁用（2）。上游用 query 参数限定「只改状态」，
// 不加这个参数会被当成整对象更新并清空其他字段。
func (a *AiPy) SetTokenStatus(ctx context.Context, c Credential, id int64, status int) error {
	if !tokenCredOK(c) {
		return &APIError{Kind: ErrAuth, Message: "缺少 Cookie 或访问令牌"}
	}
	if id <= 0 || (status != 1 && status != 2) {
		return &APIError{Kind: ErrUnknown, Message: "参数错误"}
	}
	p := "/api/token/?status_only=true"
	resp, err := a.c.DoJSON(ctx, "PUT", a.base(c)+p, a.headers(c),
		map[string]any{"id": id, "status": status})
	if err != nil {
		return &APIError{Kind: ErrNetwork, Message: "网络错误: " + err.Error()}
	}
	return aipyTokenOK(resp, p)
}

// aipyTokenOK 把「只关心成功与否」的令牌写操作响应归一成 error。
func aipyTokenOK(resp *httpx.Response, path string) error {
	if resp == nil {
		return &APIError{Kind: ErrNetwork, Message: "无响应", Path: path}
	}
	if resp.Status == 401 || resp.Status == 403 {
		return aipyTokenErr(resp, "登录已失效，请重新登录", path)
	}
	if resp.Status >= 400 {
		return aipyTokenErr(resp, "", path)
	}
	m, err := resp.JSON()
	if err != nil {
		// 2xx 但非 JSON（个别网关对 DELETE 返回空体）：视为成功
		return nil
	}
	if !envOK(m) {
		return aipyTokenErr(resp, envMsg(m), path)
	}
	return nil
}
