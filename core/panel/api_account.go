package panel

// 账号管理 / 手动导入 / 备份导出导入 API。
//
// 为什么要补这一层：网页登录容器依赖各平台的登录流程，页面结构一变就可能
// 抓不到会话，用户会卡在「添加账号」这一步出不来（实测反馈）。
// 手动粘贴凭据与备份导入是两条完全不依赖平台页面的通路，必须有。
//
// 安全边界：本服务只监听 127.0.0.1 且校验 Host（见 loopbackOnly），
// 导出内容含明文凭据，只能在本机 WebView 内显示，不对外暴露。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/provider"
	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// credentialInput 是手动导入 / 添加账号的入参。
// 字段与 provider.Credential 对齐，但都是可选：不同平台需要的凭据不同。
type credentialInput struct {
	Provider     string `json:"provider"`
	Label        string `json:"label"`
	AccountID    string `json:"account_id"`
	Cookie       string `json:"cookie"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UID          string `json:"uid"`
	EnterpriseID string `json:"enterprise_id"`
	TenantID     string `json:"tenant_id"`
	Domain       string `json:"domain"`
	// ExpiresAt 之前漏了：WorkBuddy 这类 token 型平台靠它判断要不要刷新，
	// 丢掉会导致「备份导回来 token 立刻算过期」或「永不过期」两种错。
	ExpiresAt string            `json:"expires_at,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// secret 序列化成 provider.Credential 的 JSON 形状。
//
// 用结构体 marshal 而不是手写 map：provider.Credential 以后加字段（比如新平台
// 需要的私有参数）备份/恢复能自动跟上，不会像之前那样漏掉 expires_at。
func (in credentialInput) secret() json.RawMessage {
	cred := provider.Credential{
		AccountID:    in.AccountID,
		Label:        in.Label,
		Cookie:       in.Cookie,
		AccessToken:  in.AccessToken,
		RefreshToken: in.RefreshToken,
		UID:          in.UID,
		EnterpriseID: in.EnterpriseID,
		TenantID:     in.TenantID,
		Domain:       in.Domain,
		Extra:        in.Extra,
	}
	if ts := strings.TrimSpace(in.ExpiresAt); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			cred.ExpiresAt = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", ts); err == nil {
			cred.ExpiresAt = t
		}
	}
	b, _ := json.Marshal(cred)
	return b
}

func (in credentialInput) hasSecret() bool {
	return in.Cookie != "" || in.AccessToken != "" || in.RefreshToken != ""
}

// stableAccountID 用凭据内容算稳定主键。
// 绝对不能用随机数或进程内哈希：重启后同一个账号会变成另一条记录，
// 旧记录里的凭据就再也读不回来了。
func stableAccountID(providerID, secret string) string {
	h := sha256.Sum256([]byte(providerID + "\x00" + secret))
	return hex.EncodeToString(h[:])[:16]
}

func readBody(r *http.Request, dst any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 2<<20)) // 2MB 上限
	if err != nil {
		return fmt.Errorf("读取请求体失败: %w", err)
	}
	if len(b) == 0 {
		return fmt.Errorf("请求体为空")
	}
	return json.Unmarshal(b, dst)
}

func (s *Server) requireStore(w http.ResponseWriter) bool {
	if s.opt.Store == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "存储未就绪"})
		return false
	}
	return true
}

// handleAccountAdd 手动添加一条账号。
//
// POST /api/account/add
// {"provider":"workbuddy","label":"主号","cookie":"...","access_token":"..."}
func (s *Server) handleAccountAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	var in credentialInput
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	in.Provider = strings.TrimSpace(in.Provider)
	in.Label = strings.TrimSpace(in.Label)
	if in.Provider == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "请选择平台"})
		return
	}
	if _, ok := s.opt.Reg.Get(in.Provider); !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "不支持的平台: " + in.Provider})
		return
	}
	if !in.hasSecret() {
		writeJSON(w, map[string]any{"ok": false, "error": "请填写 Cookie 或 Token"})
		return
	}
	seed := in.Cookie + in.AccessToken + in.RefreshToken
	id := in.AccountID
	if id == "" {
		id = stableAccountID(in.Provider, seed)
	}
	secret := in.secret()
	// account_id 也写进 secret，provider 层用它做请求身份头
	var m map[string]any
	_ = json.Unmarshal(secret, &m)
	m["account_id"] = id
	secret, _ = json.Marshal(m)

	a := store.Account{
		Provider:   in.Provider,
		AccountID:  id,
		Label:      in.Label,
		SecretJSON: secret,
		Enabled:    true,
	}
	if err := s.opt.Store.Put(a); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "account_id": id})
}

// handleAccountReorder 保存用户拖拽后的账号顺序。
//
// POST /api/account/reorder {"keys":[{"provider":"...","account_id":"..."}, ...]}
//
// 前端长按卡片拖动后把整份顺序提交上来，后端只认数组下标，不认具体值，
// 避免「换了个界面就排出不同结果」。
func (s *Server) handleAccountReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	re, ok := s.opt.Store.(store.Reorderer)
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "当前存储不支持排序"})
		return
	}
	var in struct {
		Keys []store.AccountKey `json:"keys"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(in.Keys) == 0 {
		writeJSON(w, map[string]any{"ok": false, "error": "缺少 keys"})
		return
	}
	if err := re.Reorder(in.Keys); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleAccountDelete 删除账号。
//
// POST /api/account/delete {"provider":"...","account_id":"..."}
func (s *Server) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	var in struct {
		Provider  string `json:"provider"`
		AccountID string `json:"account_id"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if in.Provider == "" || in.AccountID == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "缺少 provider / account_id"})
		return
	}
	if err := s.opt.Store.Delete(in.Provider, in.AccountID); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleAccountLabel 修改账号备注（label）。
//
// POST /api/account/label {"provider":"...","account_id":"...","label":"主号"}
//
// 前端长按账号名触发。走 List → 改 Label → Put，与 handleAccountToggle 同一条
// 一致性路径：SecretJSON / Enabled / Order 全部原样带回，只改备注这一个字段。
// label 允许传空串（清空备注），长度截断到 40 字符，防止超长 name 撑爆卡片。
func (s *Server) handleAccountLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	var in struct {
		Provider  string `json:"provider"`
		AccountID string `json:"account_id"`
		Label     string `json:"label"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if in.Provider == "" || in.AccountID == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "缺少 provider / account_id"})
		return
	}
	in.Label = strings.TrimSpace(in.Label)
	if len([]rune(in.Label)) > 40 {
		in.Label = string([]rune(in.Label)[:40])
	}
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for _, a := range list {
		if a.Provider == in.Provider && a.AccountID == in.AccountID {
			a.Label = in.Label
			if err := s.opt.Store.Put(a); err != nil {
				writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, map[string]any{"ok": true, "label": a.Label})
			return
		}
	}
	writeJSON(w, map[string]any{"ok": false, "error": "账号不存在"})
}

// handleAccountToggle 启用 / 停用账号。
//
// POST /api/account/toggle {"provider":"...","account_id":"...","enabled":false}
func (s *Server) handleAccountToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	var in struct {
		Provider  string `json:"provider"`
		AccountID string `json:"account_id"`
		Enabled   *bool  `json:"enabled"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if in.Provider == "" || in.AccountID == "" || in.Enabled == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "缺少参数"})
		return
	}
	if !s.requireStore(w) {
		return
	}
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for _, a := range list {
		if a.Provider == in.Provider && a.AccountID == in.AccountID {
			a.Enabled = *in.Enabled
			if err := s.opt.Store.Put(a); err != nil {
				writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, map[string]any{"ok": true, "enabled": a.Enabled})
			return
		}
	}
	writeJSON(w, map[string]any{"ok": false, "error": "账号不存在"})
}

// backupData 是备份文件的格式。导出/导入用同一结构，方便以后加字段。
type backupData struct {
	Version    int             `json:"version"`
	ExportedAt string          `json:"exported_at,omitempty"`
	Accounts   []backupAccount `json:"accounts"`
}

type backupAccount struct {
	Provider  string          `json:"provider"`
	AccountID string          `json:"account_id"`
	Label     string          `json:"label,omitempty"`
	Enabled   bool            `json:"enabled"`
	Secret    json.RawMessage `json:"secret"`
}

// handleExport 导出全部账号（含凭据）。
//
// GET /api/export
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	out := backupData{Version: 1, ExportedAt: time.Now().Format(time.RFC3339), Accounts: make([]backupAccount, 0, len(list))}
	for _, a := range list {
		out.Accounts = append(out.Accounts, backupAccount{
			Provider: a.Provider, AccountID: a.AccountID, Label: a.Label,
			Enabled: a.Enabled, Secret: a.SecretJSON,
		})
	}
	writeJSON(w, out)
}

// handleImport 从备份恢复。
//
// POST /api/import  body 可以是备份对象本身，也可以是 {"text":"<备份 JSON 文本>"}
//
// 策略：按 (provider, account_id) 覆盖写入，不删现有账号（避免误操作清空）。
// 单条格式错误只跳过该条，不影响其余账号导入。
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "读取请求体失败"})
		return
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "不是合法的 JSON"})
		return
	}
	// 兼容 {"text":"..."}：用户直接把备份文本粘进来
	if m, ok := payload.(map[string]any); ok {
		if t, ok := m["text"].(string); ok && strings.TrimSpace(t) != "" {
			if err := json.Unmarshal([]byte(t), &payload); err != nil {
				writeJSON(w, map[string]any{"ok": false, "error": "备份内容不是合法 JSON"})
				return
			}
		}
	}
	var doc backupData
	re, _ := json.Marshal(payload)
	if err := json.Unmarshal(re, &doc); err != nil || len(doc.Accounts) == 0 {
		writeJSON(w, map[string]any{"ok": false, "error": "备份里没有账号数据"})
		return
	}
	added, skipped := 0, 0
	var errs []string
	for _, b := range doc.Accounts {
		if b.Provider == "" || b.AccountID == "" || len(b.Secret) == 0 {
			skipped++
			continue
		}
		if _, ok := s.opt.Reg.Get(b.Provider); !ok {
			skipped++
			errs = append(errs, b.Provider+" 不是已支持的平台")
			continue
		}
		// secret 必须是个对象，防止把任意字符串写进账号库
		var obj map[string]any
		if err := json.Unmarshal(b.Secret, &obj); err != nil {
			skipped++
			continue
		}
		if err := s.opt.Store.Put(store.Account{
			Provider: b.Provider, AccountID: b.AccountID, Label: b.Label,
			SecretJSON: b.Secret, Enabled: b.Enabled,
		}); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		added++
	}
	res := map[string]any{"ok": true, "added": added, "skipped": skipped}
	if added == 0 {
		// 一条都没导进去：给用户明确的失败反馈，别让他以为「成功了」但列表没变
		res["ok"] = false
		res["error"] = "备份里没有可用的账号（平台不受支持或凭据格式不对）"
	}
	if len(errs) > 0 {
		res["errors"] = errs
	}
	writeJSON(w, res)
}

// handleMeta 给设置页提供版本信息。
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok": true, "version": s.opt.Version,
		"providers": len(s.opt.Reg.All()),
	})
}

// handleProbe 用一次性凭据试连，不落盘。
//
// POST /api/probe {"provider":"aipy","cookie":"..."}
// 手动导入后立刻点「验证」，能马上知道凭据对不对，不用等回到列表看错误。
func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in credentialInput
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	p, ok := s.opt.Reg.Get(in.Provider)
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "不支持的平台: " + in.Provider})
		return
	}
	if !in.hasSecret() {
		writeJSON(w, map[string]any{"ok": false, "error": "请填写 Cookie 或 Token"})
		return
	}
	var cred provider.Credential
	sec := in.secret()
	_ = json.Unmarshal(sec, &cred)
	cred.AccountID = "probe"
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	snap, qerr := p.Query(ctx, cred)
	if qerr != nil {
		snap.Error = qerr.Error()
	}
	writeJSON(w, snap)
}
