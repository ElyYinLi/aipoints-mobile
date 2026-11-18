// Package provider — Trae（国内版 SOLO）每日签到。
//
// 接口（Trae 桌面端自己用的）：
//
//	POST {base}/trae/api/v2/ug/checkin_credits/status   今天签没签、能领多少
//	POST {base}/trae/api/v2/ug/checkin_credits/claim    领取
//
// 认证：Authorization: Cloud-IDE-JWT <token>
//
// 桌面版是从本机 Trae 的 storage.json 里解密拿 JWT；手机上拿不到那个文件，
// 所以走 **WebView 登录后截获 api.trae.cn 请求头** 这条路径——登录成功后
// Trae 网页自己会发带 JWT 的请求，壳层抓一次 Authorization 即可。
//
// 对照实现：dsh-checkin-all/lib/providers/trae.js（MIT），请求头逐项对齐。
package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

const (
	traeBase       = "https://api.trae.cn"
	traeAppVersion = "0.1.51"
	traeUA         = "VSCode 1.107.1 (TRAE SOLO CN)"
)

// Trae 业务码（来自第三方实现与桌面端实测）：
//
//	0          成功
//	9004/9095  今日已签
//	9074       参与人数过多排队限流，属正常，不应报错
//	1001/1002  token 失效，需要重新登录
const (
	traeCodeOK      = 0
	traeCodeRate    = 9074
	traeCodeAuthErr = 1001
)

// Trae 实现 Provider。
type Trae struct {
	c *httpx.Client
}

// NewTrae 构造 Trae 适配器。
func NewTrae(c *httpx.Client) *Trae { return &Trae{c: c} }

// ID 返回平台标识。
func (t *Trae) ID() string { return "trae" }

// Meta 返回展示信息。
func (t *Trae) Meta() Meta {
	return Meta{ID: "trae", Name: "Trae", Auth: "token", Mobile: "ok",
		Note: "WebView 登录后截获 Cloud-IDE-JWT；纯 REST，无需设备指纹"}
}

func (t *Trae) base(c Credential) string {
	v := trimSlash(c.Domain)
	if v == "" {
		return traeBase
	}
	if hasScheme(v) {
		return v
	}
	return "https://" + v
}

// headers 与桌面端保持一致。缺这些头时服务端会返回空数据而不是报错，
// 所以宁可与客户端一模一样。
func (t *Trae) headers(c Credential) map[string]string {
	h := map[string]string{
		"Authorization":      "Cloud-IDE-JWT " + c.AccessToken,
		"Content-Type":       "application/json",
		"User-Agent":         traeUA,
		"X-User-Region":      "CN",
		"Package-Type":       "stable_cn",
		"X-Lscbd-Aid":        "787976",
		"X-Lscbd-Platform":   "windows",
		"App-Version":        traeAppVersion,
		"Accept":             "*/*",
		"X-Request-Id":       randHex(16),
		"X-Device-Brand":     "To be filled by O.E.M.",
		"X-Device-Type":      "windows",
		"X-Os-Version":       "10.0.19045",
		"X-Lgw-Req-Sdk-Type": "3",
	}
	if c.Extra != nil {
		if d := c.Extra["device_id"]; d != "" {
			h["X-Device-Id"] = d
		}
	}
	return h
}

// Query 读签到状态与可用积分。
func (t *Trae) Query(ctx context.Context, c Credential) (Snapshot, error) {
	snap := Snapshot{Provider: "trae", AccountID: c.AccountID, At: time.Now()}
	if c.AccessToken == "" {
		snap.Error = "缺少 JWT，请在登录页完成 Trae 登录"
		return snap, nil
	}
	resp, err := t.post(ctx, c, "/trae/api/v2/ug/checkin_credits/status")
	if err != nil {
		snap.Error = "网络错误: " + err.Error()
		return snap, nil
	}
	if resp.Status == 401 || resp.Status == 403 {
		snap.Error = fmt.Sprintf("登录已失效(%d)，请重新登录 Trae", resp.Status)
		return snap, nil
	}
	m, err := resp.JSON()
	if err != nil {
		snap.Error = "响应解析失败: " + err.Error()
		return snap, nil
	}
	// 业务码优先于 HTTP 状态：200 里也可能带错误码
	if code := numOf(m["code"]); code == traeCodeAuthErr || code == 1002 {
		snap.Error = "登录已失效，请重新登录 Trae"
		return snap, nil
	}
	if code := numOf(m["code"]); code != traeCodeOK && code != 0 {
		snap.Error = fmt.Sprintf("Trae 返回错误码 %s：%s", fmtNum(code), strOf(pick(m, "message", "msg")))
		return snap, nil
	}

	data := digMap(m, "data")
	src := m
	if data != nil {
		src = data
	}
	snap.CheckedIn = boolOf(pick(src, "checked_in", "checkedIn"))
	// credits 与 extra_credits 是「本次可领」与「额外赠送」两种口径，相加会把奖励算大
	credits := numOf(pick(src, "credits", "credit"))
	if credits == 0 {
		credits = numOf(pick(src, "extra_credits", "extraCredits"))
	}
	// 仅在拿到可信额度时才写余额，否则会让「合计」凭空多出一个 0
	if credits > 0 {
		snap.Balance = credits
		snap.BalanceText = fmtNum(credits) + " 积分"
	}
	if snap.CheckedIn {
		snap.Detail = "今天已签到"
	} else {
		snap.Detail = "今天还没签"
	}
	snap.OK = true
	return snap, nil
}

// Checkin 执行签到。已签、限流都算「不需要用户处理」，不算失败。
func (t *Trae) Checkin(ctx context.Context, c Credential) (Result, error) {
	res := Result{Provider: "trae", AccountID: c.AccountID}
	if c.AccessToken == "" {
		res.Error = "缺少 JWT"
		return res, nil
	}
	// 先查状态：已签就不重复请求，避免触发风控
	if snap, _ := t.Query(ctx, c); snap.CheckedIn {
		res.OK, res.Already, res.Message = true, true, "今天已签到"
		return res, nil
	}
	resp, err := t.post(ctx, c, "/trae/api/v2/ug/checkin_credits/claim")
	if err != nil {
		res.Error = "网络错误: " + err.Error()
		return res, nil
	}
	if resp.Status == 401 || resp.Status == 403 {
		res.Error = fmt.Sprintf("登录已失效(%d)，请重新登录 Trae", resp.Status)
		return res, nil
	}
	m, err := resp.JSON()
	if err != nil {
		res.Error = "响应解析失败: " + err.Error()
		return res, nil
	}
	code := numOf(m["code"])
	switch {
	case code == traeCodeOK:
		res.OK, res.Message = true, "签到成功"
	case code == 9004 || code == 9095:
		res.OK, res.Already, res.Message = true, true, "今天已签到"
	case code == traeCodeRate:
		// 排队限流是常态，短时间狂重试会延长惩罚，交给下一次打开 App
		res.Message = "签到人数较多，稍后再试"
		res.Error = ""
		res.OK = false
	case code == traeCodeAuthErr || code == 1002:
		res.Error = "登录已失效，请重新登录 Trae"
	default:
		res.Error = fmt.Sprintf("Trae 返回错误码 %s：%s", fmtNum(code), strOf(pick(m, "message", "msg")))
	}
	if res.OK {
		if data := digMap(m, "data"); data != nil {
			if v := numOf(pick(data, "points", "credits")); v > 0 {
				res.Gain = v
			}
		}
	}
	return res, nil
}

// traeBody 是签到接口要求的请求体。
// 桌面端固定发 {"req_source":1}；发空对象会被服务端判为非法请求直接拒（表现为 9074）。
func traeBody() map[string]any { return map[string]any{"req_source": 1} }

func (t *Trae) post(ctx context.Context, c Credential, path string) (*httpx.Response, error) {
	return t.c.DoJSON(ctx, "POST", t.base(c)+path, t.headers(c), traeBody())
}
