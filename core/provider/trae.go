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
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

const traeBase = "https://api.trae.cn"

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
		"User-Agent":         "VSCode 1.107.1 (TRAE SOLO CN)",
		"X-User-Region":      "CN",
		"Package-Type":       "stable_cn",
		"X-Lscbd-Aid":        "787976",
		"X-Lscbd-Platform":   "windows",
		"App-Version":        "0.1.51",
		"Accept":             "*/*",
		"X-Device-Brand":     "To be filled by O.E.M.",
		"X-Device-Type":      "windows",
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
		snap.Error = err.Error()
		return snap, nil
	}
	m, err := resp.JSON()
	if err != nil {
		snap.Error = err.Error()
		return snap, nil
	}
	data := digMap(m, "data")
	src := m
	if data != nil {
		src = data
	}

	snap.CheckedIn = boolOf(pick(src, "checked_in", "checkedIn"))
	credits := numOf(pick(src, "credits", "credit")) + numOf(pick(src, "extra_credits", "extraCredits"))
	snap.Balance = credits
	snap.BalanceText = fmtNum(credits) + " 积分"
	if snap.CheckedIn {
		snap.Detail = "今天已签到"
	} else {
		snap.Detail = "今天还没签"
	}
	return snap, nil
}

// Checkin 执行签到。已签时返回 Already，不算失败。
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
		res.Error = err.Error()
		return res, nil
	}
	if _, err := resp.JSON(); err != nil {
		res.Error = err.Error()
		return res, nil
	}
	res.OK, res.Message = true, "签到成功"
	if snap, _ := t.Query(ctx, c); snap.Balance > 0 {
		res.Gain = snap.Balance
	}
	return res, nil
}

func (t *Trae) post(ctx context.Context, c Credential, path string) (*httpx.Response, error) {
	return t.c.DoJSON(ctx, "POST", t.base(c)+path, t.headers(c), map[string]any{})
}
