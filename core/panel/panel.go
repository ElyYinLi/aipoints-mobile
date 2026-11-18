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
}

// Server 是面板实例。
type Server struct {
	opt Options
	srv *http.Server
}

// New 构造面板。
func New(opt Options) *Server {
	if opt.Addr == "" {
		opt.Addr = "127.0.0.1:7879"
	}
	if opt.Reg == nil {
		opt.Reg = provider.NewRegistry(httpx.New())
	}
	s := &Server{opt: opt}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/providers", s.handleProviders)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/query", s.handleQuery)
	mux.HandleFunc("/api/checkin", s.handleCheckin)
	mux.HandleFunc("/api/login/start", s.handleLoginStart)
	mux.HandleFunc("/api/health", s.handleHealth)
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
		out[i] = provider.Snapshot{Provider: a.Provider, AccountID: a.AccountID, At: time.Now()}
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
		go func(i int, p provider.Provider, cred provider.Credential) {
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
			out[i] = snap
		}(i, p, cred)
	}
	wg.Wait()
	writeJSON(w, out)
}

// handleCheckin 串行签到（同设备并发最容易被风控）。
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
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		res, cerr := p.Checkin(ctx, cred)
		cancel()
		if cerr != nil {
			res.Error = cerr.Error()
		}
		if res.AccountID == "" {
			res.AccountID = a.AccountID
		}
		if res.Provider == "" {
			res.Provider = a.Provider
		}
		out = append(out, res)
		// 只有真的发过写请求才需要间隔（防同设备批量写触发风控）。
		// 登录失效 / 网络不通这类账号根本没写入，白等一秒多只会让「一键签到」显得卡。
		if wroteRemote(res) {
			time.Sleep(1200 * time.Millisecond)
		}
	}
	writeJSON(w, out)
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
