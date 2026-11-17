// Package provider 定义各平台统一接入契约。
//
// 设计原则：
//  1. 引擎不做 UI、不碰平台差异，只负责「拿凭据 → 调官方接口 → 归一化结果」。
//  2. 所有网络解析都必须容错：官方字段随时会改名，缺失时降级而不是 panic。
//  3. 凭据只在内存里使用，不写日志（见各 provider 的 logf 约定）。
package provider

import (
	"context"
	"encoding/json"
	"time"
)

// Credential 是一个账号的凭据快照。字段按平台择用，未用到的留空。
//
// 注意：移动端的持久化由壳层负责（iOS Keychain / Android Keystore），
// 引擎收到的 Credential 只在内存中存活一次调用。
type Credential struct {
	AccountID string `json:"account_id"`
	Label     string `json:"label,omitempty"`

	// Cookie 型凭据（灵犀 / 百度搭子 / Trae 会话）
	Cookie string `json:"cookie,omitempty"`

	// Token 型凭据（WorkBuddy / MiniMax / Qoder）
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`

	// 平台身份头（WorkBuddy 需要）
	UID          string `json:"uid,omitempty"`
	EnterpriseID string `json:"enterprise_id,omitempty"`
	TenantID     string `json:"tenant_id,omitempty"`

	// Domain 允许覆盖默认域名（CN / 国际双站点平台必填）
	Domain string `json:"domain,omitempty"`

	// Extra 放各平台私有参数（设备号、区域、客户端版本等）
	Extra map[string]string `json:"extra,omitempty"`
}

// Snapshot 是一次查询的归一化结果。
type Snapshot struct {
	Provider    string          `json:"provider"`
	AccountID   string          `json:"account_id"`
	OK          bool            `json:"ok"`
	Balance     float64         `json:"balance"`
	BalanceText string          `json:"balance_text,omitempty"`
	CheckedIn   bool            `json:"checked_in"`
	Streak      int             `json:"streak,omitempty"`
	Detail      string          `json:"detail,omitempty"`
	Error       string          `json:"error,omitempty"`
	Raw         json.RawMessage `json:"-"`
	At          time.Time       `json:"at"`
}

// Result 是一次签到归并结果。
type Result struct {
	Provider  string  `json:"provider"`
	AccountID string  `json:"account_id"`
	OK        bool    `json:"ok"`
	Already   bool    `json:"already"`
	Gain      float64 `json:"gain,omitempty"`
	Message   string  `json:"message,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// Meta 描述一个平台的元信息，供 UI 渲染与能力判断。
type Meta struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Auth   string `json:"auth"`   // cookie / token / page
	Mobile string `json:"mobile"` // ok / conditional / blocked
	Note   string `json:"note,omitempty"`
}

// Provider 是所有平台适配器必须实现的接口。
type Provider interface {
	Meta() Meta
	// Query 只读：取余额 + 今日签到状态。实现必须自身兜底，尽量不返回 error。
	Query(ctx context.Context, c Credential) (Snapshot, error)
	// Checkin 执行签到。写操作，调用方必须先确认今日未签。
	Checkin(ctx context.Context, c Credential) (Result, error)
}
