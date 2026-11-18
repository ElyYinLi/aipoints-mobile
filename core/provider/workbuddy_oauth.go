package provider

// WorkBuddy 官方浏览器 OAuth（设备码变体，无本地回调）。
//
// 为什么必须走这条而不是抓 cookie：
// WorkBuddy 的 API 用 `Authorization: Bearer <access token>` 鉴权，而网页登录
// 只会往 localStorage 写页面自己的会话，**不会**产生可用的 access token。
// 用户实测「点完成取不到 access token」正是这个原因。
//
// 契约来自多个独立开源实现的交叉验证
// （workbuddy-switch / workbuddy2api / dsh-model / creditdaddy）：
//
//	POST {base}/v2/plugin/auth/state?platform=workbuddy
//	     headers: X-No-Authorization / X-User-Id / X-Enterprise-Id / X-Department-Info = true
//	     → {code:0, data:{state, authUrl}}
//	GET  {base}/v2/plugin/auth/token?state=<state>
//	     → 未授权 code=11217 "login ing..."；授权完成后 data 内含 access token
//	GET  {base}/v2/plugin/login/account?state=<state>  (Bearer <token>)
//	     → uid / 企业 / 租户 / 昵称，这几项决定后续请求的身份头

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

const (
	wbOAuthPlatform = "workbuddy"
	// wbCodeWaiting 是「授权还没完成」的业务码。它不是错误，要继续轮询。
	wbCodeWaiting = 11217
	wbUA          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// WBOAuthStart 是一次 OAuth 的起始信息。
type WBOAuthStart struct {
	State   string
	AuthURL string
}

// WBOAuthToken 是换到的令牌。
type WBOAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    float64
}

// WBOAuthAccount 是账号身份信息。
type WBOAuthAccount struct {
	UID          string
	EnterpriseID string
	TenantID     string
	Nickname     string
}

// oauthHeaders 是申请 state / 轮询 token 用的头。
//
// 实测（2026-11-18 线上）：只要 `X-No-Authorization: true` 就够。
// 若再补 X-User-Id / X-Enterprise-Id / X-Department-Info，服务端会把字面量
// "true" 当成真实企业 ID，直接 403 enterprise not found —— 千万别加。
func (w *WorkBuddy) oauthHeaders(c Credential) map[string]string {
	h := w.host(c)
	return map[string]string{
		"Accept":             "application/json, text/plain, */*",
		"X-No-Authorization": "true",
		"Origin":             "https://" + h,
		"Referer":            "https://" + h + "/",
		"User-Agent":         wbUA,
	}
}

// OAuthStart 申请一次性 state 与授权地址。
func (w *WorkBuddy) OAuthStart(ctx context.Context, c Credential) (WBOAuthStart, error) {
	var out WBOAuthStart
	u := w.base(c) + "/v2/plugin/auth/state?platform=" + wbOAuthPlatform
	resp, err := w.c.DoJSON(ctx, "POST", u, w.oauthHeaders(c), map[string]any{})
	if err != nil {
		return out, fmt.Errorf("网络错误: %w", err)
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return out, fmt.Errorf("上游返回 HTTP %d", resp.Status)
	}
	m, err := resp.JSON()
	if err != nil {
		return out, fmt.Errorf("响应解析失败: %w", err)
	}
	if !envOK(m) {
		return out, fmt.Errorf("上游拒绝: %s", envMsg(m))
	}
	data := digMap(m, "data")
	if data == nil {
		data = m
	}
	out.State = strOf(pick(data, "state", "auth_state", "authState"))
	out.AuthURL = strOf(pick(data, "authUrl", "auth_url", "url", "authURL"))
	if out.State == "" || out.AuthURL == "" {
		return out, fmt.Errorf("上游未返回 state/authUrl: %s", envMsg(m))
	}
	return out, nil
}

// OAuthPoll 查一次令牌。waiting=true 表示用户还没完成授权，应继续轮询。
func (w *WorkBuddy) OAuthPoll(ctx context.Context, c Credential, state string) (WBOAuthToken, bool, error) {
	var out WBOAuthToken
	u := w.base(c) + "/v2/plugin/auth/token?state=" + url.QueryEscape(state)
	resp, err := w.c.DoJSON(ctx, "GET", u, w.oauthHeaders(c), nil)
	if err != nil {
		return out, false, fmt.Errorf("网络错误: %w", err)
	}
	if resp.Status == 401 || resp.Status == 403 {
		return out, false, fmt.Errorf("鉴权失败(%d)", resp.Status)
	}
	m, err := resp.JSON()
	if err != nil {
		return out, false, fmt.Errorf("响应解析失败: %w", err)
	}
	code, hasCode := httpx.Num(m["code"])
	msg := envMsg(m)
	if hasCode && code == wbCodeWaiting {
		return out, true, nil
	}
	if hasCode && code != 0 && code != 200 {
		return out, false, fmt.Errorf("业务码 %s: %s", fmtNum(code), msg)
	}
	data := digMap(m, "data")
	if data == nil {
		data = m
	}
	out.AccessToken = strOf(pick(data, "access_token", "accessToken", "token", "accessTokenValue"))
	if out.AccessToken == "" {
		// 没有业务码但也没有 token：仍视为等待，避免误报失败
		return out, true, nil
	}
	out.RefreshToken = strOf(pick(data, "refresh_token", "refreshToken"))
	out.ExpiresIn = numOf(pick(data, "expires_in", "expiresIn"))
	return out, false, nil
}

// OAuthAccount 用已换到的 token 取账号身份（uid / 企业 / 租户）。
// 失败不致命：只是身份头缺失，签到接口多数账号仍可用。
func (w *WorkBuddy) OAuthAccount(ctx context.Context, c Credential, state, token string) (WBOAuthAccount, error) {
	var out WBOAuthAccount
	h := w.host(c)
	// 这个接口要真 Bearer：不能带 X-No-Authorization，否则会被当成匿名请求
	hd := map[string]string{
		"Accept":        "application/json, text/plain, */*",
		"Authorization": "Bearer " + token,
		"Origin":        "https://" + h,
		"Referer":       "https://" + h + "/",
		"User-Agent":    wbUA,
	}
	u := w.base(c) + "/v2/plugin/login/account?state=" + url.QueryEscape(state)
	resp, err := w.c.DoJSON(ctx, "GET", u, hd, nil)
	if err != nil {
		return out, err
	}
	m, err := resp.JSON()
	if err != nil {
		return out, err
	}
	data := digMap(m, "data")
	if data == nil {
		data = digMap(m, "account")
	}
	if data == nil {
		data = m
	}
	out.UID = strOf(pick(data, "uid", "user_id", "userId", "userid", "id"))
	out.EnterpriseID = strOf(pick(data, "enterprise_id", "enterpriseId", "enterprise", "ent_id"))
	out.TenantID = strOf(pick(data, "tenant_id", "tenantId", "tenant", "tenantid"))
	out.Nickname = strOf(pick(data, "nickname", "nick_name", "name", "display_name", "username", "user_name"))
	return out, nil
}
