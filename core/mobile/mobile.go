// Package mobile 是 gomobile bind 的导出层。
//
// gomobile 只支持有限的类型（string / int / bool / []byte），所以这里统一用
// JSON 字符串进出，壳层（Swift / Kotlin）负责序列化与 UI。
//
// 导出清单（供 Swift/Kotlin 对照）：
//
//	Start(dataDir string) string       初始化引擎，返回 {"ok":true,"version":"..."}
//	Providers() string                 平台元信息数组
//	PutAccount(json string) string     新增/更新账号凭据
//	ListAccounts() string              账号列表（不含 secret）
//	QueryAll() string                  并发查询全部启用账号
//	CheckinOne(provider, account string) string
//	CheckinAll() string                按账号顺序串行签到（同平台自动错峰由壳层控时）
//	Plan(provider string, dayOffset int) string 当日随机排期
package mobile

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
	"github.com/ElyYinLi/aipoints-mobile/core/panel"
	"github.com/ElyYinLi/aipoints-mobile/core/provider"
	"github.com/ElyYinLi/aipoints-mobile/core/scheduler"
	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// Version 由壳层显示，构建时可通过 -ldflags 注入。
var Version = "0.3.2"

type engine struct {
	mu     sync.Mutex
	dir    string
	reg    *provider.Registry
	client *httpx.Client
	store  *store.FileStore
	panel  *panel.Server
}

var (
	inst *engine
	once sync.Once
)

// PanelAddr 是内嵌面板的默认监听地址。壳层 WebView 直接加载 http://127.0.0.1:7879
const PanelAddr = "127.0.0.1:7879"

// Start 初始化引擎并拉起内嵌面板。重复调用返回同一实例。
func Start(dataDir string) string {
	once.Do(func() {
		client := httpx.New()
		reg := provider.NewRegistry(client)
		e := &engine{dir: dataDir, client: client, reg: reg}
		if st, err := store.NewFileStore(filepath.Join(dataDir, "core")); err == nil {
			e.store = st
		}
		if e.store != nil {
			p := panel.New(panel.Options{
				Addr:     PanelAddr,
				Store:    e.store,
				Reg:      reg,
				LoginURL: loginURL,
				Version:  Version,
			})
			if err := p.Start(); err == nil {
				e.panel = p
			}
		}
		inst = e
	})
	return mustJSON(map[string]any{"ok": true, "version": Version, "dir": dataDir, "panel": "http://" + PanelAddr})
}

// PanelURL 返回面板地址，未启动时返回空串。
func PanelURL() string {
	if inst == nil || inst.panel == nil {
		return ""
	}
	return "http://" + PanelAddr
}

// Providers 返回平台元信息。
func Providers() string {
	if inst == nil {
		return errJSON("engine not started")
	}
	return mustJSON(inst.reg.All())
}

// PutAccount 写入一条账号凭据。参数是 provider.Account 的 JSON。
//
// 安全约定：移动端壳层应先把 secret 交 Keychain 保管，这里传的是解密后的明文，
// 用完即弃；FileStore 只在桌面/CLI 场景使用。
func PutAccount(accountJSON string) string {
	if inst == nil {
		return errJSON("engine not started")
	}
	var a store.Account
	if err := json.Unmarshal([]byte(accountJSON), &a); err != nil {
		return errJSON("bad account json: " + err.Error())
	}
	if a.Provider == "" || a.AccountID == "" {
		return errJSON("provider/account_id required")
	}
	if a.SecretJSON == nil {
		return errJSON("secret required")
	}
	if inst.store == nil {
		return errJSON("store unavailable")
	}
	if err := inst.store.Put(a); err != nil {
		return errJSON(err.Error())
	}
	return mustJSON(map[string]any{"ok": true})
}

// ListAccounts 返回账号（secret 已剥离）。
func ListAccounts() string {
	if inst == nil || inst.store == nil {
		return errJSON("engine not ready")
	}
	list, err := inst.store.List()
	if err != nil {
		return errJSON(err.Error())
	}
	type view struct {
		Provider  string `json:"provider"`
		AccountID string `json:"account_id"`
		Label     string `json:"label"`
		Enabled   bool   `json:"enabled"`
	}
	out := make([]view, 0, len(list))
	for _, a := range list {
		out = append(out, view{a.Provider, a.AccountID, a.Label, a.Enabled})
	}
	return mustJSON(out)
}

// QueryAll 并发查询所有启用账号，单账号失败不影响其他账号。
func QueryAll() string {
	if inst == nil || inst.store == nil {
		return errJSON("engine not ready")
	}
	list, err := inst.store.List()
	if err != nil {
		return errJSON(err.Error())
	}
	var wg sync.WaitGroup
	results := make([]provider.Snapshot, len(list))
	for i, a := range list {
		if !a.Enabled {
			continue
		}
		p, ok := inst.reg.Get(a.Provider)
		if !ok {
			continue
		}
		var cred provider.Credential
		if err := json.Unmarshal(a.SecretJSON, &cred); err != nil {
			results[i] = provider.Snapshot{Provider: a.Provider, AccountID: a.AccountID, Error: "凭据损坏"}
			continue
		}
		if cred.AccountID == "" {
			cred.AccountID = a.AccountID
		}
		wg.Add(1)
		go func(idx int, p provider.Provider, cred provider.Credential, acc store.Account) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			snap, err := p.Query(ctx, cred)
			if err != nil {
				snap = provider.Snapshot{Provider: acc.Provider, AccountID: acc.AccountID, Error: err.Error()}
			}
			if snap.Provider == "" {
				snap.Provider = acc.Provider
			}
			if snap.AccountID == "" {
				snap.AccountID = acc.AccountID
			}
			snap.At = time.Now()
			results[idx] = snap
		}(i, p, cred, a)
	}
	wg.Wait()

	// 清掉未启用账号留下的零值，避免 UI 出现空卡片
	out := make([]provider.Snapshot, 0, len(results))
	for _, r := range results {
		if r.Provider != "" || r.Error != "" {
			out = append(out, r)
		}
	}
	return mustJSON(out)
}

// CheckinOne 对指定账号签到。
func CheckinOne(providerID, accountID string) string {
	if inst == nil || inst.store == nil {
		return errJSON("engine not ready")
	}
	p, ok := inst.reg.Get(providerID)
	if !ok {
		return errJSON("unknown provider: " + providerID)
	}
	list, err := inst.store.List()
	if err != nil {
		return errJSON(err.Error())
	}
	for _, a := range list {
		if a.Provider != providerID || a.AccountID != accountID {
			continue
		}
		var cred provider.Credential
		if err := json.Unmarshal(a.SecretJSON, &cred); err != nil {
			return errJSON("凭据损坏")
		}
		if cred.AccountID == "" {
			cred.AccountID = accountID
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := p.Checkin(ctx, cred)
		if err != nil {
			res.Error = err.Error()
		}
		return mustJSON(res)
	}
	return errJSON("account not found")
}

// CheckinAll 串行签到。故意串行：同设备批量并发最容易触发风控。
func CheckinAll() string {
	if inst == nil || inst.store == nil {
		return errJSON("engine not ready")
	}
	list, err := inst.store.List()
	if err != nil {
		return errJSON(err.Error())
	}
	out := make([]provider.Result, 0, len(list))
	for _, a := range list {
		if !a.Enabled {
			continue
		}
		var one provider.Result
		if err := json.Unmarshal([]byte(CheckinOne(a.Provider, a.AccountID)), &one); err != nil {
			one = provider.Result{Provider: a.Provider, AccountID: a.AccountID, Error: err.Error()}
		}
		out = append(out, one)
		// 平台之间留一点间隔，避免请求过于密集
		time.Sleep(1200 * time.Millisecond)
	}
	return mustJSON(out)
}

// Plan 返回某平台当日随机排期（dayOffset=0 是今天）。
func Plan(providerID string, dayOffset int) string {
	if inst == nil || inst.store == nil {
		return errJSON("engine not ready")
	}
	list, err := inst.store.List()
	if err != nil {
		return errJSON(err.Error())
	}
	var ids []string
	for _, a := range list {
		if a.Provider == providerID && a.Enabled {
			ids = append(ids, a.AccountID)
		}
	}
	now := time.Now()
	day := now.AddDate(0, 0, dayOffset)
	start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, day.Location())
	end := start.Add(2*time.Hour + 30*time.Minute) // 09:00 - 11:30，与桌面版默认区间一致
	return mustJSON(scheduler.Daily(day, providerID, ids, start, end))
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return errJSON(err.Error())
	}
	return string(b)
}

func errJSON(msg string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error": msg})
	return string(b)
}

// loginURLs 是各平台的登录入口。面板点「添加账号」时，壳层据此打开官方页面。
//
// 原则：一律打开平台自己的登录页，不自己实现账号密码表单——
// 密码只流向平台，我们不接触；平台改登录流程也不用跟着改。
var loginURLs = map[string]string{
	// WorkBuddy 是 token 型：网页登录拿不到 access token，必须走官方 OAuth
	// （POST /v2/plugin/auth/state → authUrl → 轮询 /v2/plugin/auth/token）。
	// 这里指向本机 302 端点，由引擎实时换取 authUrl 再跳转。
	"workbuddy": "http://127.0.0.1:7879/login/workbuddy",
	"lingxi":    "https://www.kdocs.cn/",
	// 业务站在 dumate.baidu.com；wenxin.baidu.com 是文心一言，页面上没有搭子登录入口。
	"dumate": "https://dumate.baidu.com/",
	"trae":   "https://www.trae.cn/",
	"aipy":   "https://www.trustoken.cn/",
}

// loginURL 供面板查询某平台的登录入口。
func loginURL(id string) string { return loginURLs[id] }
