package mobile

// 账号管理（供壳层调用）：DeleteAccount / SetEnabled / EngineDir
// 这三个操作壳层本来也可以自己拼 FileStore 的 JSON，但导出成方法更省事，
// 且能保证与 PutAccount 走同一条一致性路径。

import (
	"encoding/json"

	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// DeleteAccount 删除一条账号。
func DeleteAccount(providerID, accountID string) string {
	if inst == nil || inst.store == nil {
		return errJSON("engine not ready")
	}
	if err := inst.store.Delete(providerID, accountID); err != nil {
		return errJSON(err.Error())
	}
	return mustJSON(map[string]any{"ok": true})
}

// SetEnabled 启用/停用一条账号（停用后不参与查询与签到）。
func SetEnabled(providerID, accountID string, enabled bool) string {
	if inst == nil || inst.store == nil {
		return errJSON("engine not ready")
	}
	list, err := inst.store.List()
	if err != nil {
		return errJSON(err.Error())
	}
	for _, a := range list {
		if a.Provider == providerID && a.AccountID == accountID {
			a.Enabled = enabled
			if err := inst.store.Put(store.Account{
				Provider: a.Provider, AccountID: a.AccountID, Label: a.Label,
				SecretJSON: a.SecretJSON, Enabled: a.Enabled,
			}); err != nil {
				return errJSON(err.Error())
			}
			return mustJSON(map[string]any{"ok": true, "enabled": enabled})
		}
	}
	return errJSON("account not found")
}

// AddAccount 是 PutAccount 的便利版本：壳层只要给出 plain 字段，
// 由 Go 侧拼成 provider.Credential 再落库。
//
// 参数是 JSON：{"provider","account_id","label","cookie","token","uid","enterprise","domain"}
func AddAccount(j string) string {
	var in struct {
		Provider  string `json:"provider"`
		AccountID string `json:"account_id"`
		Label     string `json:"label"`
		Cookie    string `json:"cookie"`
		Token     string `json:"token"`
		UID       string `json:"uid"`
		Ent       string `json:"enterprise"`
		Tenant    string `json:"tenant"`
		Domain    string `json:"domain"`
	}
	if err := json.Unmarshal([]byte(j), &in); err != nil {
		return errJSON("bad json: " + err.Error())
	}
	if in.Provider == "" || in.AccountID == "" {
		return errJSON("provider/account_id required")
	}
	secret, _ := json.Marshal(map[string]string{
		"account_id":    in.AccountID,
		"label":         in.Label,
		"cookie":        in.Cookie,
		"access_token":  in.Token,
		"uid":           in.UID,
		"enterprise_id": in.Ent,
		"tenant_id":     in.Tenant,
		"domain":        in.Domain,
	})
	return PutAccount(mustJSON(store.Account{
		Provider: in.Provider, AccountID: in.AccountID, Label: in.Label,
		SecretJSON: secret, Enabled: true,
	}))
}

// EngineDir 返回引擎数据目录，壳层用它定位调试日志。
func EngineDir() string {
	if inst == nil {
		return ""
	}
	return inst.dir
}
