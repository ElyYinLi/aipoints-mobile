package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// Dumate 百度搭子适配器（Cookie 型）。
//
// 接口来源：AI积分助手 v2.2.0 静态逆向，其又移植自 totorosir-dumate-checkin v1.1.0。
//
//	GET  /api/dumate/points/loginBonusInfo   签到状态
//	POST /api/dumate/points/loginBonus       领取每日登录积分
//	GET  /api/dumate/points/quota_overview   积分总览（可用 = 总额 - 已用）
//
// 写接口需要 x-bce-request-id（百度 BCE 网关的幂等 ID），用随机 hex 即可。
type Dumate struct {
	c *httpx.Client
}

// NewDumate 构造适配器。
func NewDumate(c *httpx.Client) *Dumate { return &Dumate{c: c} }

const dumateBase = "https://dumate.baidu.com"

func (d *Dumate) Meta() Meta {
	return Meta{
		ID: "dumate", Name: "百度搭子", Auth: "cookie", Mobile: "ok",
		Note: "纯 cookie + REST；百度账号 WebView 登录后直连",
	}
}

func (d *Dumate) base(c Credential) string {
	v := strings.TrimSpace(c.Domain)
	if v == "" {
		return dumateBase
	}
	v = strings.TrimSuffix(v, "/")
	if strings.Contains(v, "://") {
		return v
	}
	return "https://" + v
}

func (d *Dumate) headers(c Credential) map[string]string {
	h := map[string]string{
		"Cookie":     c.Cookie,
		"Origin":     d.base(c),
		"Referer":    d.base(c) + "/app",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	}
	// 关键：写接口必须带 csrftoken 头，值取自 bce-user-info cookie（去掉首尾引号）。
	// 缺它时服务端会拒绝，且错误信息不直观（容易被当成 cookie 失效）。
	if tok := csrfFromCookie(c.Cookie); tok != "" {
		h["csrftoken"] = tok
	}
	return h
}

// csrfFromCookie 从 cookie 串里取 bce-user-info 的值作为 csrftoken。
// cookie 形如：a=1; bce-user-info="%7B%22token%22%3A...%22%7D"; c=2
func csrfFromCookie(cookie string) string {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		name, value, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(name) != "bce-user-info" {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}

// Query 读签到状态 + 可用积分。
func (d *Dumate) Query(ctx context.Context, c Credential) (Snapshot, error) {
	snap := Snapshot{Provider: "dumate", AccountID: c.AccountID, At: time.Now()}
	if c.Cookie == "" {
		snap.Error = "缺少 cookie"
		return snap, nil
	}
	resp, err := d.c.DoJSON(ctx, "GET", d.base(c)+"/api/dumate/points/loginBonusInfo", d.headers(c), nil)
	if err != nil {
		snap.Error = "网络错误: " + err.Error()
		return snap, nil
	}
	// 状态码先于 JSON 解析：401/403 的响应体不是 JSON，先解析会误报成解析失败
	if resp.Status == 401 || resp.Status == 403 {
		snap.Error = fmt.Sprintf("登录已失效(%d)，请重新登录百度搭子", resp.Status)
		return snap, nil
	}
	m, err := resp.JSON()
	if err != nil {
		snap.Error = "响应解析失败: " + err.Error()
		return snap, nil
	}
	// 百度网关的响应可能包在 data / result 里，字段名也可能是几种写法，统一下钻。
	snap.CheckedIn = boolOf(digAny(m, "signed_today", "claimed", "today_signed", "is_signed", "login_bonus_claimed", "received"))
	if v, ok := httpx.Num(digAny(m, "gain", "points", "reward", "bonus")); ok && v > 0 {
		snap.Detail = "今日可得 " + fmtNum(v)
	}
	// 状态接口本身可能就带积分总额，先填上；额度总览接口拿到更准确的值再覆盖
	if v, ok := httpx.Num(digAny(m, "total_points", "points_total", "total")); ok {
		snap.Balance = v
		snap.BalanceText = fmtNum(v)
	}
	if total, used, ok := d.quota(ctx, c); ok {
		snap.Balance = total - used
		snap.BalanceText = fmtNum(snap.Balance)
	}
	snap.Unit = "积分"
	snap.OK = true
	return snap, nil
}

// quota 返回 (总额, 已用)。
func (d *Dumate) quota(ctx context.Context, c Credential) (float64, float64, bool) {
	resp, err := d.c.DoJSON(ctx, "GET", d.base(c)+"/api/dumate/points/quota_overview", d.headers(c), nil)
	if err != nil {
		return 0, 0, false
	}
	m, err := resp.JSON()
	if err != nil {
		return 0, 0, false
	}
	total, ok1 := httpx.Num(digAny(m, "total_points", "total", "quota_total"))
	used, ok2 := httpx.Num(digAny(m, "used_points", "used", "quota_used"))
	if !ok1 {
		return 0, 0, false
	}
	if !ok2 {
		used = 0
	}
	return total, used, true
}

// Checkin 领每日登录积分。
func (d *Dumate) Checkin(ctx context.Context, c Credential) (Result, error) {
	res := Result{Provider: "dumate", AccountID: c.AccountID}
	if c.Cookie == "" {
		res.Error = "缺少 cookie"
		return res, nil
	}
	snap, _ := d.Query(ctx, c)
	if snap.CheckedIn {
		res.OK = true
		res.Already = true
		res.Message = "今日已签到"
		return res, nil
	}
	h := d.headers(c)
	h["x-bce-request-id"] = randHex(16)
	resp, err := d.c.DoJSON(ctx, "POST", d.base(c)+"/api/dumate/points/loginBonus", h, map[string]any{})
	if err != nil {
		res.Error = "网络不通，稍后重试"
		return res, nil
	}
	if resp.Status == 401 || resp.Status == 403 {
		res.Error = fmt.Sprintf("登录已失效(%d)，请重新登录百度搭子", resp.Status)
		return res, nil
	}
	m, err := resp.JSON()
	if err != nil {
		res.Error = "响应解析失败: " + err.Error()
		return res, nil
	}
	msgD := strOf(pick(m, "msg", "message"))
	if code, ok := httpx.Num(m["code"]); ok && code != 0 {
		// 百度系业务码 0 表示成功。已经领过常以业务码报错形式返回，按已签处理。
		if strings.Contains(msgD, "已领") || strings.Contains(msgD, "已签") || strings.Contains(msgD, "重复") {
			res.OK, res.Already, res.Message = true, true, msgD
			return res, nil
		}
		res.Error = fmt.Sprintf("业务码 %s: %s", fmtNum(code), msgD)
		return res, nil
	}
	res.OK = true
	res.Message = "签到成功"
	if v, ok := httpx.Num(httpx.Pick(m, "data", "gain")); ok {
		res.Gain = v
	}
	return res, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

var _ Provider = (*Dumate)(nil)
