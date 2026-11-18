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
	Provider  string `json:"provider"`
	AccountID string `json:"account_id"`
	// Label 是账号备注（主号 / 小号等），由面板在查询时从账号库回填，
	// provider 本身不产生这个字段。前端据此区分多个同平台账号。
	Label       string  `json:"label,omitempty"`
	OK          bool    `json:"ok"`
	Balance     float64 `json:"balance"`
	BalanceText string  `json:"balance_text,omitempty"`
	// Unit 是余额单位文案，空表示「积分」（各家默认口径一致时才留空）。
	Unit      string          `json:"unit,omitempty"`
	CheckedIn bool            `json:"checked_in"`
	Streak    int             `json:"streak,omitempty"`
	Detail    string          `json:"detail,omitempty"`
	Error     string          `json:"error,omitempty"`
	Raw       json.RawMessage `json:"-"`
	At        time.Time       `json:"at"`
}

// Result 是一次签到归并结果。
type Result struct {
	Provider  string  `json:"provider"`
	AccountID string  `json:"account_id"`
	OK        bool    `json:"ok"`
	Already   bool    `json:"already"`
	Gain      float64 `json:"gain,omitempty"`
	// Unit 是本次奖励的单位文案（灵石 / 积分 / 点）。空表示沿用平台默认口径。
	Unit    string `json:"unit,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Meta 描述一个平台的元信息，供 UI 渲染与能力判断。
type Meta struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Auth   string `json:"auth"`   // cookie / token / page
	Mobile string `json:"mobile"` // ok / conditional / blocked
	Note   string `json:"note,omitempty"`
	// Tasks 表示该平台支持「每日任务领取」（实现了 TaskClaimer）。
	// 前端据此决定是否在账号卡片上显示「任务」按钮。
	Tasks bool `json:"tasks,omitempty"`
	// Lottery 表示该平台支持「签到后抽奖」（实现了 LotteryProvider）。
	// 抽奖随「一键签到」自动执行，前端不需要额外按钮。
	Lottery bool `json:"lottery,omitempty"`
}

// Provider 是所有平台适配器必须实现的接口。
type Provider interface {
	Meta() Meta
	// Query 只读：取余额 + 今日签到状态。实现必须自身兜底，尽量不返回 error。
	Query(ctx context.Context, c Credential) (Snapshot, error)
	// Checkin 执行签到。写操作，调用方必须先确认今日未签。
	Checkin(ctx context.Context, c Credential) (Result, error)
}

// Task 是平台「每日任务」里的一项（AiPy 的 /api/user/daily_tasks/status）。
type Task struct {
	// TaskType 是领取时回传上游的标识。
	TaskType string `json:"task_type"`
	Title    string `json:"title"`
	// Reward 是该任务可得奖励，单位见 Unit（AiPy 已换算为灵石）。
	Reward   float64 `json:"reward"`
	Progress int     `json:"progress"`
	Target   int     `json:"target"`
	// Status 上游原样返回：locked（未达标）/ claimed（已领取）。
	Status string `json:"status"`
	// Claimable 是否当前可领取（进度达标且未领取）。
	Claimable bool `json:"claimable"`
	// Unit 奖励单位文案（AiPy 是「灵石」，换算已在 provider 内完成）。
	Unit string `json:"unit,omitempty"`
}

// TaskClaimer 是「支持每日任务领取」的平台才实现的扩展接口。
// 不是所有平台都有任务体系（WorkBuddy 等就没有），所以不放进
// Provider 主接口，用类型断言按需启用，前端据此决定是否显示「任务」按钮。
type TaskClaimer interface {
	Provider
	// Tasks 只读：返回该账号当日任务列表。
	Tasks(ctx context.Context, c Credential) ([]Task, error)
	// ClaimTask 领取指定任务奖励，返回本次获得的奖励（平台口径）。
	ClaimTask(ctx context.Context, c Credential, taskType string) (Result, error)
}

// DrawStatus 是「每日抽奖」的当前状态。
type DrawStatus struct {
	// Remaining 是剩余可抽次数。签到后一般会 +1。
	Remaining int `json:"remaining"`
	// Prize 是最近一次抽中的奖品文案（没有则空）。
	Prize string `json:"prize,omitempty"`
}

// DrawResult 是一次抽奖的结果。
type DrawResult struct {
	OK bool `json:"ok"`
	// Remaining 抽完后剩余次数（上游没返回时为 -1，表示未知）。
	Remaining int `json:"remaining"`
	// Prize 本次抽中的奖品（积分 / 会员 / 实物券等，上游文案原样透传）。
	Prize string `json:"prize,omitempty"`
	// Gain 若奖品是积分，这里给出数值；非积分奖品为 0。
	Gain  float64 `json:"gain,omitempty"`
	Unit  string  `json:"unit,omitempty"`
	Error string  `json:"error,omitempty"`
}

// LotteryProvider 是「支持签到后抽奖」的平台才实现的扩展接口。
// 百度搭子每次签到给一次抽奖机会，属于这类平台。
type LotteryProvider interface {
	Provider
	// DrawStatus 只读：剩余抽奖次数。
	DrawStatus(ctx context.Context, c Credential) (DrawStatus, error)
	// Draw 抽一次奖。实现内部必须保证幂等键每次全新，否则第二次会被判重复。
	Draw(ctx context.Context, c Credential) (DrawResult, error)
}
