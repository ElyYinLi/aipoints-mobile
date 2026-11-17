package mobile

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/provider"
)

// Probe 用一次性凭据试连，不落盘、不进账号库。
// 壳层在「WebView 登录完成 → 提取 cookie/token → 确认可用」这一步调用它。
//
// 参数是 provider.Credential 的 JSON；返回该平台的 Snapshot。
func Probe(providerID, credentialJSON string) string {
	if inst == nil {
		return errJSON("engine not started")
	}
	p, ok := inst.reg.Get(providerID)
	if !ok {
		return errJSON("unknown provider: " + providerID)
	}
	var cred provider.Credential
	if err := json.Unmarshal([]byte(credentialJSON), &cred); err != nil {
		return errJSON("bad credential json: " + err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snap, err := p.Query(ctx, cred)
	if err != nil {
		snap = provider.Snapshot{Provider: providerID, Error: err.Error()}
	}
	if snap.Provider == "" {
		snap.Provider = providerID
	}
	snap.At = time.Now()
	return mustJSON(snap)
}

// Summary 返回跨平台汇总，供壳层首页顶部大数字直接渲染。
//
//	{"total_balance": 1234.5, "ok": 5, "failed": 1, "checked_in": 3, "accounts": 6}
func Summary() string {
	if inst == nil || inst.store == nil {
		return errJSON("engine not ready")
	}
	var snaps []provider.Snapshot
	if err := json.Unmarshal([]byte(QueryAll()), &snaps); err != nil {
		return errJSON(err.Error())
	}
	sum := struct {
		TotalBalance float64 `json:"total_balance"`
		OK           int     `json:"ok"`
		Failed       int     `json:"failed"`
		CheckedIn    int     `json:"checked_in"`
		Accounts     int     `json:"accounts"`
	}{Accounts: len(snaps)}
	for _, s := range snaps {
		if s.Error != "" {
			sum.Failed++
			continue
		}
		sum.OK++
		sum.TotalBalance += s.Balance
		if s.CheckedIn {
			sum.CheckedIn++
		}
	}
	return mustJSON(sum)
}
