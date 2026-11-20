// Package panel 提供一个零依赖的本机 HTTP 面板，供 iOS / Android 壳层用 WebView 加载。
//
// 为什么是本地面板而不是纯原生 UI：
//   - 双端 UI 只写一份，改版成本最低；
//   - 壳层保持极薄（一个 WebView + 一个启动调用），平台差异降到最小；
//   - 后续若要做原生 UI，直接调 mobile.go 导出的 JSON 接口即可，引擎不用动。
//
// 只监听 127.0.0.1，不对外暴露；不引入任何前端构建链（单文件 HTML + 原生 fetch）。
package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
	"github.com/ElyYinLi/aipoints-mobile/core/provider"
	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// Options 是面板启动参数。
type Options struct {
	Addr  string // 默认 127.0.0.1:7879
	Store store.Store
	Reg   *provider.Registry
	// LoginURL 返回某平台的登录入口（由壳层注入）。为空时面板隐藏「登录」入口。
	LoginURL func(providerID string) string
	// Version 由壳层注入，设置页显示，方便确认装的是哪一版。
	Version string
	// DataDir 是持久化目录（与账号库同级）。为空则不落盘，gain memo 只在内存里。
	DataDir string
}

// Server 是面板实例。
type Server struct {
	opt Options
	srv *http.Server
	// oauth 保存进行中的 WorkBuddy OAuth state（内存态，进程结束即丢）。
	// key 是平台 id，value 是服务端签发的一次性 state。
	oauthMu sync.Mutex
	oauth   map[string]oauthPending
	// gains 记住每个账号「今天签到拿了多少」，用于状态行显示「今日已签 +N 积分」。
	// 平台接口不一定回这个数，所以本地兜一层；跨天自动失效。
	gains *gainMemo
	// tasks 记住「今天已经成功领过哪些任务奖励」，用于阻断重复申领。
	// 上游 status 会滞后，只靠它挡不住（见 task_memo.go）。
	tasks *taskMemo
	// busy 记录正在写上游的账号键 "provider|account"。
	// 用户连点两下「一键签到」会起两个 goroutine 同时打上游，
	// 这正是「请求太多容易黑号」的直接来源，必须串行化。
	busyMu sync.Mutex
	busy   map[string]bool
	// notify 是结果通知（Webhook）配置与发送器。
	notify *notifier
}

// New 构造面板。
func New(opt Options) *Server {
	if opt.Addr == "" {
		opt.Addr = "127.0.0.1:7879"
	}
	if opt.Reg == nil {
		opt.Reg = provider.NewRegistry(httpx.New())
	}
	s := &Server{opt: opt, oauth: make(map[string]oauthPending), busy: make(map[string]bool)}
	if opt.DataDir != "" {
		s.gains = newGainMemo(opt.DataDir)
		s.tasks = newTaskMemo(opt.DataDir)
		s.notify = newNotifier(opt.DataDir)
	} else {
		s.tasks = newTaskMemo("")
		s.notify = newNotifier("")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/providers", s.handleProviders)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/query", s.handleQuery)
	mux.HandleFunc("/api/checkin", s.handleCheckin)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/api/tasks/claim", s.handleTaskClaim)
	mux.HandleFunc("/api/tasks/claim_all", s.handleTaskClaimAll)

	// 结果通知（Webhook）
	mux.HandleFunc("/api/notify", s.handleNotify)
	mux.HandleFunc("/api/notify/config", s.handleNotifyConfig)
	mux.HandleFunc("/api/notify/test", s.handleNotifyTest)
	mux.HandleFunc("/api/notify/reset", s.handleNotifyReset)

	// API 令牌（密钥）管理，仅 AiPy Pro 平台支持
	mux.HandleFunc("/api/tokens", s.handleTokens)
	mux.HandleFunc("/api/tokens/save", s.handleTokenSave)
	mux.HandleFunc("/api/tokens/delete", s.handleTokenDelete)
	mux.HandleFunc("/api/tokens/status", s.handleTokenStatus)
	// 抽奖：百度搭子签到后送抽奖机会，随「一键签到」一起抽掉；
	// 另给卡片上的「抽奖」按钮提供查询剩余次数 / 手动抽一次的入口
	// （用户要求能看见「还有几次、抽到了什么」，不能只有静默批量）。
	mux.HandleFunc("/api/lottery/status", s.handleLotteryStatus)
	mux.HandleFunc("/api/lottery/draw", s.handleLotteryDraw)
	mux.HandleFunc("/api/lottery/draw_all", s.handleLotteryDrawAll)
	mux.HandleFunc("/api/login/start", s.handleLoginStart)
	mux.HandleFunc("/api/health", s.handleHealth)
	// 运行日志：出问题时用户不用装任何调试工具，设置页里直接看请求/错误，
	// 也能一键复制发给开发者定位。日志在写入侧已脱敏，不含凭据。
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/logs/clear", s.handleLogsClear)
	// 账号管理与备份：手动导入凭据、删除、启停、试连、导出/导入。
	// 这几条通路完全不依赖平台网页登录，是「添加账号」的可靠兜底。
	mux.HandleFunc("/api/account/add", s.handleAccountAdd)
	mux.HandleFunc("/api/account/delete", s.handleAccountDelete)
	mux.HandleFunc("/api/account/toggle", s.handleAccountToggle)
	mux.HandleFunc("/api/account/reorder", s.handleAccountReorder)
	mux.HandleFunc("/api/probe", s.handleProbe)
	mux.HandleFunc("/api/export", s.handleExport)
	mux.HandleFunc("/api/import", s.handleImport)
	mux.HandleFunc("/api/meta", s.handleMeta)
	// WorkBuddy 的 OAuth 通路：
	//   /login/workbuddy   → 302 到官方 authUrl（壳层直接加载这个地址）
	//   /api/oauth/start   → 同上，但返回 JSON，给面板自绘流程用
	//   /api/oauth/finish  → 轮询换 token 并落库（用户点「完成」后调用）
	mux.HandleFunc("/login/", s.handleLoginRedirect)
	mux.HandleFunc("/api/oauth/start", s.handleOAuthStart)
	mux.HandleFunc("/api/oauth/finish", s.handleOAuthFinish)
	// 本地回环服务，但 WebView 里的页面同源可访问：加 Host 校验，挡住 DNS rebinding
	// （攻击者把域名解析到 127.0.0.1 并从网页里读走账号数据）。
	s.srv = &http.Server{
		Addr:              opt.Addr,
		Handler:           loopbackOnly(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	return s
}

// loopbackOnly 只接受 Host 为回环地址的请求。
//
// 为什么需要：服务只监听 127.0.0.1，但浏览器里任何网页都能向 127.0.0.1 发请求。
// DNS rebinding 能让攻击者域名解析到本机，从而读到 /api/query 里的账号信息。
// 校验 Host 头即可挡住这类请求（WebView 加载的是 127.0.0.1:port，Host 必然匹配）。
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		switch host {
		case "127.0.0.1", "localhost", "::1", "[::1]":
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "forbidden host", http.StatusForbidden)
		}
	})
}

// Start 在后台启动，立即返回。端口被占用时返回错误。
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.opt.Addr)
	if err != nil {
		return fmt.Errorf("面板端口 %s 不可用: %w", s.opt.Addr, err)
	}
	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("panel: %v", err)
		}
	}()
	return nil
}

// Stop 优雅关闭。
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 本地单文件，禁缓存，避免壳层升级后还加载旧界面
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(indexHTML))
}

// handleHealth 供壳层在加载 WebView 前轮询，确认引擎已就绪。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "ts": time.Now().Unix()})
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.opt.Reg.All())
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if s.opt.Store == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "未配置存储"})
		return
	}
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
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
	writeJSON(w, out)
}

// handleLoginStart 告诉面板「该打开哪个登录页」。
//
// 面板是纯网页，拉不起原生登录容器。所以这里只返回描述，由壳层通过
// 自定义 scheme（points://login?provider=xxx）接管，打开官方登录页。
func (s *Server) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("provider")
	if id == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "缺少 provider"})
		return
	}
	url := ""
	if s.opt.LoginURL != nil {
		url = s.opt.LoginURL(id)
	}
	writeJSON(w, map[string]any{"ok": url != "", "provider": id, "url": url})
}

// handleQuery 并发查询全部启用账号。
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// 并行查询：串行时 N 个账号的耗时是叠加的，弱网下面板会长时间停在骨架屏，
	// 这正是「界面卡顿」的根因。每个账号独立超时，单账号慢不拖累其他账号。
	// 结果按原顺序回填，前端渲染顺序保持稳定。
	out := make([]provider.Snapshot, len(list))
	var wg sync.WaitGroup
	for i, a := range list {
		out[i] = provider.Snapshot{Provider: a.Provider, AccountID: a.AccountID, Label: a.Label, At: time.Now()}
		if !a.Enabled {
			out[i].Error = "已停用"
			continue
		}
		p, ok := s.opt.Reg.Get(a.Provider)
		if !ok {
			out[i].Error = "未支持的平台"
			continue
		}
		cred, err := decodeCred(a)
		if err != nil {
			out[i].Error = "凭据损坏"
			continue
		}
		wg.Add(1)
		go func(i int, p provider.Provider, cred provider.Credential, label string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			snap, qerr := p.Query(ctx, cred)
			if qerr != nil {
				snap.Error = qerr.Error()
			}
			if snap.AccountID == "" {
				snap.AccountID = out[i].AccountID
			}
			if snap.Provider == "" {
				snap.Provider = out[i].Provider
			}
			if snap.At.IsZero() {
				snap.At = time.Now()
			}
			// 备注（label）不在 provider 查询结果里，这里从账号记录回填，
			// 否则前端卡片永远只能显示一长串 account_id，账号多了认不出谁是谁。
			if snap.Label == "" {
				snap.Label = label
			}
			out[i] = snap
		}(i, p, cred, a.Label)
	}
	wg.Wait()
	// 兜底：平台没给 today_gain 时用本地记住的值补上，
	// 否则状态行只能显示「今日已签」，看不到拿了多少。
	//
	// 关于「能不能顺手纠正状态」——这里有个必须守住的边界（0.5.4 修复）：
	//   - memo 是**本次进程亲手签下**的（Fresh）：可以纠正。WorkBuddy 实测
	//     签完后状态接口仍回 checked_in=false，不纠正就会出现「签到成功」toast
	//     配「今日未签到」状态行，前后矛盾。
	//   - memo 只是从磁盘读回来的旧记录：**绝不能改状态**。之前无条件
	//     `CheckedIn = true`，导致平台明确说「未签」的账号被强行改成「已签」，
	//     于是前端认为不需要签，永远不发起签到——用户看到的就是「签没签都显示
	//     同一个状态」，还以为是软件坏了。
	for i := range out {
		if out[i].Error != "" {
			continue
		}
		// 平台直接给了今日积分 → 顺手记下来，明天这个账号就算状态接口不回数也有值
		if out[i].TodayGain > 0 && out[i].CheckedIn {
			s.gains.Set(out[i].Provider, out[i].AccountID, out[i].TodayGain)
			continue
		}
		if out[i].TodayGain > 0 {
			continue
		}
		g := s.gains.Get(out[i].Provider, out[i].AccountID)
		if g <= 0 {
			continue
		}
		// 数值可以补（平台不给具体数时用来显示 +N）
		out[i].TodayGain = g
		// 状态只有在「本条 memo 是本次进程签下的」时才允许纠正
		if !out[i].CheckedIn && s.gains.Fresh(out[i].Provider, out[i].AccountID) {
			out[i].CheckedIn = true
			if out[i].Detail == "" || out[i].Detail == "今日未签到" {
				out[i].Detail = "今日已签"
			}
		}
	}
	writeJSON(w, out)
}

// handleCheckin 串行签到（同设备并发最容易被风控）。
//
// 三重约束：
//  1. 串行：一个账号签完再下一个；
//  2. 写后节流：真的发过 POST 才延时 1.2s；
//  3. 账号级互斥：同一账号正在签时，第二次请求直接返回 skipped，
//     不发第二个请求。用户连点两下「一键签到」是最常见的误操作，
//     不挡的话就是双倍请求量——正是「请求太多容易黑号」的直接来源。
func (s *Server) handleCheckin(w http.ResponseWriter, r *http.Request) {
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	target := r.URL.Query().Get("account")
	out := make([]provider.Result, 0, len(list))
	for _, a := range list {
		if !a.Enabled {
			continue
		}
		if target != "" && a.AccountID != target {
			continue
		}
		p, ok := s.opt.Reg.Get(a.Provider)
		if !ok {
			continue
		}
		cred, err := decodeCred(a)
		if err != nil {
			out = append(out, provider.Result{Provider: a.Provider, AccountID: a.AccountID, Error: "凭据损坏"})
			continue
		}
		key := a.Provider + "|" + a.AccountID
		if !s.acquire(key) {
			out = append(out, provider.Result{
				Provider: a.Provider, AccountID: a.AccountID,
				Message: "该账号正在签到中，已跳过",
			})
			continue
		}
		res := s.checkinOne(r.Context(), p, a, cred)
		s.release(key)
		out = append(out, res)
		// 只有真的发过写请求才需要间隔（防同设备批量写触发风控）。
		// 登录失效 / 网络不通这类账号根本没写入，白等一秒多只会让「一键签到」显得卡。
		if wroteRemote(res) {
			time.Sleep(1200 * time.Millisecond)
		}
	}
	writeJSON(w, out)
}

// checkinOne 对一个账号执行「签到 → 二次核对」。
//
// 二次核对为什么必须有：上游偶尔会返回 200 且 success=true，但服务端并没有
// 真记账（限流、灰度、并发覆盖都会这样）。不核对就会写一笔「成功但没到账」
// 的假账，用户第二天发现积分没涨，比直接报失败更糟。
//
// 核对方式：签完再查一次 Query，看 CheckedIn 是否真的变成 true。
func (s *Server) checkinOne(ctx context.Context, p provider.Provider, a store.Account, cred provider.Credential) provider.Result {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, cerr := p.Checkin(ctx, cred)
	if cerr != nil {
		res.Error = cerr.Error()
		res.Kind = provider.AsAPIError(cerr).Kind
	}
	if res.AccountID == "" {
		res.AccountID = a.AccountID
	}
	if res.Provider == "" {
		res.Provider = a.Provider
	}
	// 只有「真的提交了写请求且声称成功」才值得回查。
	// 已签 / 失败 / 风控都需要查——那三种查了也是白花一次请求。
	if res.OK && !res.Already && res.Error == "" {
		if !s.confirmCheckin(ctx, p, cred) {
			res.OK = false
			res.Kind = provider.ErrUnknown
			res.Message = "签到请求已提交，但未能确认到账，请稍后下拉刷新核对"
			if res.Error == "" {
				res.Error = res.Message
			}
			return res
		}
	}
	// 签到成功（含「已签」但明确了收益）时记住今天拿了多少，
	// 后续每次查询都能把这句「今日已签 +N 积分」补全。
	if res.OK && res.Gain > 0 {
		s.gains.Set(res.Provider, res.AccountID, res.Gain)
	}
	return res
}

// confirmCheckin 二次核对：以服务端状态为准。
//
// 回查本身失败（网络抖动）时不判定为「没到账」——那会把成功误报成失败，
// 只能算「无法确认」，保持原结果并交给用户下拉刷新。这里返回 true 表示
// 「没发现反证」。
func (s *Server) confirmCheckin(ctx context.Context, p provider.Provider, cred provider.Credential) bool {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	snap, err := p.Query(cctx, cred)
	if err != nil {
		return true
	}
	if snap.Error != "" {
		return true
	}
	return snap.CheckedIn
}

// acquire 尝试占用某账号的签到权。已被占用返回 false。
func (s *Server) acquire(key string) bool {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	if s.busy[key] {
		return false
	}
	s.busy[key] = true
	return true
}

func (s *Server) release(key string) {
	s.busyMu.Lock()
	delete(s.busy, key)
	s.busyMu.Unlock()
}

// wroteRemote 判断本次签到是否真的打到了上游的写接口。
// 已签 / 登录失效 / 缺凭据都属于「没写」，不需要节流。
func wroteRemote(r provider.Result) bool {
	if r.Error != "" || r.Already {
		return false
	}
	return r.OK
}

func decodeCred(a store.Account) (provider.Credential, error) {
	var cred provider.Credential
	if len(a.SecretJSON) == 0 {
		return cred, fmt.Errorf("empty secret")
	}
	if err := json.Unmarshal(a.SecretJSON, &cred); err != nil {
		return cred, err
	}
	if cred.AccountID == "" {
		cred.AccountID = a.AccountID
	}
	return cred, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
