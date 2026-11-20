package panel

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/provider"
	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// findAccount 按 provider + account_id 找账号。找不到返回 false。
//
// 引擎是多平台的，account_id 只在同一 provider 内唯一，所以必须带 provider。
func (s *Server) findAccount(providerID, accountID string) (store.Account, bool) {
	list, err := s.opt.Store.List()
	if err != nil {
		return store.Account{}, false
	}
	for _, a := range list {
		if a.AccountID != accountID {
			continue
		}
		if providerID != "" && a.Provider != providerID {
			continue
		}
		return a, true
	}
	return store.Account{}, false
}

// tokenManager 取出账号对应的 TokenManager 实现。第二个返回值是给用户看的错误。
func (s *Server) tokenManager(providerID, accountID string) (provider.TokenManager, store.Account, string) {
	a, ok := s.findAccount(providerID, accountID)
	if !ok {
		return nil, store.Account{}, "账号不存在"
	}
	p, ok := s.opt.Reg.Get(a.Provider)
	if !ok {
		return nil, a, "未支持的平台"
	}
	tm, ok := p.(provider.TokenManager)
	if !ok {
		return nil, a, "该平台没有 API 令牌管理"
	}
	return tm, a, ""
}

// handleTokens 列出某账号的令牌。
//
// GET /api/tokens?provider=aipy&account=<account_id>
//
// 返回里带 in_use_key：前端据此把「当前账号正在用的那条」标出来并禁止删除。
// 用户可能直接用 sk-xxx 当登录凭证，删掉它账号立刻失效。
func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	pid := r.URL.Query().Get("provider")
	aid := r.URL.Query().Get("account")
	if aid == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "缺少 account"})
		return
	}
	tm, a, msg := s.tokenManager(pid, aid)
	if tm == nil {
		writeJSON(w, map[string]any{"ok": false, "error": msg, "tokens": []any{}})
		return
	}
	cred, err := decodeCred(a)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "凭据损坏", "tokens": []any{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	list, lerr := tm.ListTokens(ctx, cred)
	if lerr != nil {
		ae := provider.AsAPIError(lerr)
		writeJSON(w, map[string]any{
			"ok": false, "error": ae.Message, "kind": string(ae.Kind), "tokens": []any{},
		})
		return
	}
	if list == nil {
		list = []provider.APIToken{}
	}
	// 当前账号正在用的那条 key（可能是空）。
	inUse := strings.TrimSpace(cred.AccessToken)
	writeJSON(w, map[string]any{
		"ok": true, "provider": a.Provider, "account_id": a.AccountID,
		"tokens": list, "in_use_key": inUse, "usable": inUse != "",
	})
}

// handleTokenSave 新建或编辑令牌。
//
// POST /api/tokens/save {"provider":"aipy","account":"...","token":{...}}
func (s *Server) handleTokenSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	var in struct {
		Provider string              `json:"provider"`
		Account  string              `json:"account"`
		Token    provider.TokenInput `json:"token"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	tm, a, msg := s.tokenManager(in.Provider, in.Account)
	if tm == nil {
		writeJSON(w, map[string]any{"ok": false, "error": msg})
		return
	}
	if strings.TrimSpace(in.Token.Name) == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "令牌名称不能为空"})
		return
	}
	cred, err := decodeCred(a)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "凭据损坏"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	if in.Token.ID > 0 {
		if uerr := tm.UpdateToken(ctx, cred, in.Token); uerr != nil {
			ae := provider.AsAPIError(uerr)
			writeJSON(w, map[string]any{"ok": false, "error": ae.Message, "kind": string(ae.Kind)})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "updated": true, "message": "已保存"})
		return
	}
	tok, cerr := tm.CreateToken(ctx, cred, in.Token)
	if cerr != nil {
		ae := provider.AsAPIError(cerr)
		writeJSON(w, map[string]any{"ok": false, "error": ae.Message, "kind": string(ae.Kind)})
		return
	}
	// 新建成功：把 key 回给前端展示一次。上游的完整 key 只在创建响应里出现。
	writeJSON(w, map[string]any{
		"ok": true, "created": true, "token": tok,
		"message": "令牌已创建，请立刻复制保存",
	})
}

// handleTokenDelete 删除令牌。
//
// POST /api/tokens/delete {"provider":"aipy","account":"...","id":123,"force":false}
//
// 删除保护：如果这条令牌的 key 正好等于账号当前正在使用的凭证，
// 直接拒绝。用户可能把 sk-xxx 当登录凭据用，删掉账号立刻 401。
// force=true 可强行删除（前端需要二次确认 + 明确告知后果）。
func (s *Server) handleTokenDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	var in struct {
		Provider string `json:"provider"`
		Account  string `json:"account"`
		ID       int64  `json:"id"`
		Force    bool   `json:"force"`
		// Key 由前端带上（列表里已有），避免为了比对再去拉一次列表
		Key string `json:"key"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if in.ID <= 0 {
		writeJSON(w, map[string]any{"ok": false, "error": "缺少令牌 ID"})
		return
	}
	tm, a, msg := s.tokenManager(in.Provider, in.Account)
	if tm == nil {
		writeJSON(w, map[string]any{"ok": false, "error": msg})
		return
	}
	cred, err := decodeCred(a)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "凭据损坏"})
		return
	}
	if !in.Force && in.Key != "" && strings.TrimSpace(cred.AccessToken) != "" &&
		strings.TrimSpace(in.Key) == strings.TrimSpace(cred.AccessToken) {
		writeJSON(w, map[string]any{
			"ok": false, "in_use": true,
			"error": "这条令牌正是该账号当前登录使用的凭证，删除后账号会立即失效。请先改用 Cookie 登录，或在确认后强制删除。",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if derr := tm.DeleteToken(ctx, cred, in.ID); derr != nil {
		ae := provider.AsAPIError(derr)
		writeJSON(w, map[string]any{"ok": false, "error": ae.Message, "kind": string(ae.Kind)})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "message": "已删除"})
}

// handleTokenStatus 启用 / 禁用令牌。
//
// POST /api/tokens/status {"provider":"aipy","account":"...","id":123,"status":1}
func (s *Server) handleTokenStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	var in struct {
		Provider string `json:"provider"`
		Account  string `json:"account"`
		ID       int64  `json:"id"`
		Status   int    `json:"status"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if in.ID <= 0 || (in.Status != 1 && in.Status != 2) {
		writeJSON(w, map[string]any{"ok": false, "error": "参数错误"})
		return
	}
	tm, a, msg := s.tokenManager(in.Provider, in.Account)
	if tm == nil {
		writeJSON(w, map[string]any{"ok": false, "error": msg})
		return
	}
	cred, err := decodeCred(a)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "凭据损坏"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if serr := tm.SetTokenStatus(ctx, cred, in.ID, in.Status); serr != nil {
		ae := provider.AsAPIError(serr)
		writeJSON(w, map[string]any{"ok": false, "error": ae.Message, "kind": string(ae.Kind)})
		return
	}
	verb := "已启用"
	if in.Status == 2 {
		verb = "已禁用"
	}
	writeJSON(w, map[string]any{"ok": true, "message": "令牌" + verb})
}
