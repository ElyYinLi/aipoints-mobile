package panel

// WorkBuddy OAuth 的本机通路。
//
// 背景：WorkBuddy 的接口用 Bearer token 鉴权，而网页登录不会把它写进 cookie，
// 所以「网页登录 + 抓 cookie」这条老路对它根本无效（用户实测取不到凭证）。
// 正确姿势是官方设备码式 OAuth：
//
//	1. 本机 POST /login/workbuddy        → 302 到官方 authUrl（壳层 WebView 加载）
//	2. 用户在页面里完成授权
//	3. 前端反复 POST /api/oauth/finish    → 每次向upstream查一次，拿到 token 才落库
//
// 采用「单次轮询 + 前端重试」而不是后端长时间阻塞：面板的 fetch 有 25s 超时，
// 后端若阻塞 45s，用户只会看到「请求超时」。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/provider"
	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// oauthPending 是一次进行中的 OAuth。
type oauthPending struct {
	state string
	at    time.Time
}

func (s *Server) workbuddyProvider() (*provider.WorkBuddy, bool) {
	if s.opt.Reg == nil {
		return nil, false
	}
	p, ok := s.opt.Reg.Get("workbuddy")
	if !ok {
		return nil, false
	}
	wb, ok := p.(*provider.WorkBuddy)
	return wb, ok
}

// startWBOAuth 申请 state + authUrl，并把 state 记到内存。
func (s *Server) startWBOAuth(ctx context.Context) (string, error) {
	wb, ok := s.workbuddyProvider()
	if !ok {
		return "", fmt.Errorf("WorkBuddy 适配器未注册")
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	st, err := wb.OAuthStart(cctx, provider.Credential{})
	if err != nil {
		return "", err
	}
	s.oauthMu.Lock()
	s.oauth["workbuddy"] = oauthPending{state: st.State, at: time.Now()}
	s.oauthMu.Unlock()
	return st.AuthURL, nil
}

// handleLoginRedirect 把 /login/{provider} 变成本平台真正的登录页。
//
// 壳层（iOS / Android）只认一个固定 URL，所以 WorkBuddy 的 OAuth authUrl
// 只能在这里现取现跳：壳层加载 /login/workbuddy，本机 302 到 authUrl。
func (s *Server) handleLoginRedirect(w http.ResponseWriter, r *http.Request) {
	pid := strings.Trim(strings.TrimPrefix(r.URL.Path, "/login/"), "/")
	if pid == "" {
		http.NotFound(w, r)
		return
	}
	if pid == "workbuddy" {
		authURL, err := s.startWBOAuth(r.Context())
		if err != nil {
			// 拿不到 authUrl 时退回官网，至少让用户能看到页面手动登录
			http.Redirect(w, r, "https://www.workbuddy.cn/", http.StatusFound)
			return
		}
		http.Redirect(w, r, authURL, http.StatusFound)
		return
	}
	if s.opt.LoginURL == nil {
		http.NotFound(w, r)
		return
	}
	u := s.opt.LoginURL(pid)
	if u == "" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

// handleOAuthStart POST /api/oauth/start {"provider":"workbuddy"} → {ok,url}
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Provider string `json:"provider"`
	}
	_ = readBody(r, &in)
	if strings.TrimSpace(in.Provider) != "workbuddy" {
		writeJSON(w, map[string]any{"ok": false, "error": "该平台不需要 OAuth"})
		return
	}
	u, err := s.startWBOAuth(r.Context())
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "url": u})
}

// handleOAuthFinish POST /api/oauth/finish {"provider":"workbuddy"}
//
// 只查一次上游：waiting=true 表示授权还没完成，前端隔 2.5s 再来。
func (s *Server) handleOAuthFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	var in struct {
		Provider  string `json:"provider"`
		Label     string `json:"label"`
		AccountID string `json:"account_id"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	pid := strings.TrimSpace(in.Provider)
	if pid != "workbuddy" {
		writeJSON(w, map[string]any{"ok": false, "error": "该平台不需要 OAuth"})
		return
	}
	wb, ok := s.workbuddyProvider()
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "WorkBuddy 适配器未注册"})
		return
	}

	s.oauthMu.Lock()
	pd, has := s.oauth[pid]
	s.oauthMu.Unlock()
	if !has || pd.state == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "没有进行中的登录，请重新点「网页登录」"})
		return
	}
	if time.Since(pd.at) > 20*time.Minute {
		s.oauthMu.Lock()
		delete(s.oauth, pid)
		s.oauthMu.Unlock()
		writeJSON(w, map[string]any{"ok": false, "error": "登录已超时，请重新点「网页登录」"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	tok, waiting, err := wb.OAuthPoll(ctx, provider.Credential{}, pd.state)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "换取 token 失败：" + err.Error()})
		return
	}
	if waiting || tok.AccessToken == "" {
		writeJSON(w, map[string]any{"ok": false, "waiting": true, "error": "等待授权…"})
		return
	}

	// 身份信息失败不致命，只是缺 X-User-Id 等头，多数账号仍可签到
	acc, _ := wb.OAuthAccount(ctx, provider.Credential{}, pd.state, tok.AccessToken)

	label := strings.TrimSpace(in.Label)
	if label == "" {
		label = acc.Nickname
	}
	id := strings.TrimSpace(in.AccountID)
	if id == "" {
		id = stableAccountID(pid, tok.AccessToken)
	}
	ci := credentialInput{
		Provider:     pid,
		Label:        label,
		AccountID:    id,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		UID:          acc.UID,
		EnterpriseID: acc.EnterpriseID,
		TenantID:     acc.TenantID,
	}
	secret := ci.secret()
	var m map[string]any
	_ = json.Unmarshal(secret, &m)
	m["account_id"] = id
	secret, _ = json.Marshal(m)

	if err := s.opt.Store.Put(store.Account{
		Provider:   pid,
		AccountID:  id,
		Label:      label,
		SecretJSON: secret,
		Enabled:    true,
	}); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.oauthMu.Lock()
	delete(s.oauth, pid)
	s.oauthMu.Unlock()

	writeJSON(w, map[string]any{
		"ok":         true,
		"account_id": id,
		"label":      label,
		"uid":        acc.UID,
		"nickname":   acc.Nickname,
	})
}
