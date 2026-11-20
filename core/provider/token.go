package provider

import "context"

// APIToken 是平台控制台里的「API 密钥」（AiPy/Trustoken 的 sk-xxx）。
//
// 注意与 Credential.AccessToken 区分：
//   - Credential.AccessToken 是**登录态凭证**（OAuth access token / Bearer），
//     我们用它在平台上做签到这类账号操作；
//   - APIToken 是用户创建的**调用密钥**，用来调网关消耗额度，与登录态无关。
//
// 但有一种情况会交叉：用户可能直接把 APIToken 填进 Credential.AccessToken 当
// 登录凭证用。此时令牌列表里会出现「当前正在使用的这一条」，删掉它会导致账号
// 立刻失效——panel 层有删除保护（见 api_tokens.go）。
type APIToken struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Key 是完整密钥。上游可能已脱敏，前端默认打码显示。
	Key string `json:"key"`
	// Status 上游口径：1 = 启用，2 = 禁用。
	Status       int     `json:"status"`
	RemainQuota  float64 `json:"remain_quota"`
	UsedQuota    float64 `json:"used_quota"`
	Unlimited    bool    `json:"unlimited_quota"`
	ModelLimits  string  `json:"model_limits"`
	AllowIPs     string  `json:"allow_ips"`
	CreatedTime  int64   `json:"created_time"`
	AccessedTime int64   `json:"accessed_time"`
	// ExpiredTime 是秒级时间戳，-1 表示永不过期。
	ExpiredTime int64 `json:"expired_time"`
}

// Enabled 按上游口径判断是否启用。
func (t APIToken) Enabled() bool { return t.Status == 1 }

// NeverExpires 是否永不过期。
func (t APIToken) NeverExpires() bool { return t.ExpiredTime < 0 }

// TokenInput 是新建 / 编辑令牌的请求体。
//
// 刻意只暴露手机上填得动的字段。model_limits / allow_ips 是字符串（逗号或
// 换行分隔），放在「高级」折叠区里可编辑——手机上虽然不舒适，但删掉能力
// 意味着用户改错一个 IP 必须开电脑，不划算。
type TokenInput struct {
	ID          int64   `json:"id,omitempty"`
	Name        string  `json:"name"`
	RemainQuota float64 `json:"remain_quota"`
	// ExpiredTime 秒级时间戳；0 由实现方补成 -1（永不过期），
	// 上游把 0 当成「已过期」，直接传会建出一条立刻失效的令牌。
	ExpiredTime int64  `json:"expired_time"`
	Unlimited   bool   `json:"unlimited_quota"`
	ModelLimits string `json:"model_limits"`
	AllowIPs    string `json:"allow_ips"`
}

// TokenManager 是「支持 API 密钥管理」的平台才实现的扩展接口。
//
// 与 TaskClaimer / LotteryProvider 同构：不放进 Provider 主接口，
// 面板用类型断言探测，Meta.Tokens 由 Registry.All() 自动推断。
// 目前只有 AiPy Pro 实现。
type TokenManager interface {
	Provider
	// ListTokens 列出该账号的全部令牌。实现需容忍上游 data 是数组或
	// {items:[],total:N} 两种形状。
	ListTokens(ctx context.Context, c Credential) ([]APIToken, error)
	// CreateToken 新建令牌。返回创建后的对象（含 key），
	// 因为上游只在创建响应里给一次完整 key。
	CreateToken(ctx context.Context, c Credential, in TokenInput) (APIToken, error)
	// UpdateToken 编辑令牌（名称、额度、过期时间、限制）。
	UpdateToken(ctx context.Context, c Credential, in TokenInput) error
	// DeleteToken 删除令牌。不可撤销。
	DeleteToken(ctx context.Context, c Credential, id int64) error
	// SetTokenStatus 启用（1）/ 禁用（2）。
	SetTokenStatus(ctx context.Context, c Credential, id int64, status int) error
}
