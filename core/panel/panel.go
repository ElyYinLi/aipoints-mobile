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
	s.srv = &http.Server{Addr: opt.Addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return s
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
	_, _ = w.Write([]byte(indexHTML))
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

// handleQuery 并发查询全部启用账号。
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	out := make([]provider.Snapshot, 0, len(list))
	for _, a := range list {
		if !a.Enabled {
			continue
		}
		p, ok := s.opt.Reg.Get(a.Provider)
		if !ok {
			continue
		}
		cred, err := decodeCred(a)
		if err != nil {
			out = append(out, provider.Snapshot{Provider: a.Provider, AccountID: a.AccountID, Error: "凭据损坏"})
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		snap, qerr := p.Query(ctx, cred)
		cancel()
		if qerr != nil {
			snap.Error = qerr.Error()
		}
		if snap.AccountID == "" {
			snap.AccountID = a.AccountID
		}
		if snap.Provider == "" {
			snap.Provider = a.Provider
		}
		snap.At = time.Now()
		out = append(out, snap)
	}
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
		time.Sleep(1200 * time.Millisecond)
	}
	writeJSON(w, out)
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

// indexHTML 是单文件面板。刻意做得极简：无框架、无外部 CDN（离线可用）。
const indexHTML = `<!DOCTYPE html>
<html lang="zh-CN"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>AI 积分</title>
<style>
:root{--bg:#f4f5f7;--card:#fff;--ink:#15181d;--sub:#6b7280;--line:#e5e7eb;--acc:#2f6bff;--ok:#0f7b4f;--bad:#c2410c}
@media(prefers-color-scheme:dark){:root{--bg:#111318;--card:#191c23;--ink:#e8eaed;--sub:#9aa3b2;--line:#262b35;--acc:#5b8bff}}
*{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.5 -apple-system,"PingFang SC",system-ui,sans-serif;padding:14px 14px calc(84px + env(safe-area-inset-bottom))}
header{display:flex;align-items:baseline;justify-content:space-between;margin:4px 2px 14px}
h1{font-size:19px;margin:0;letter-spacing:-.01em}
.sum{font-size:13px;color:var(--sub)}
.sum b{color:var(--ink);font-size:17px;font-variant-numeric:tabular-nums}
.card{background:var(--card);border:1px solid var(--line);border-radius:14px;padding:14px 16px;margin-bottom:10px}
.row{display:flex;justify-content:space-between;align-items:center;gap:10px}
.name{font-weight:600;display:flex;align-items:center;gap:8px}
.dot{width:8px;height:8px;border-radius:50%;background:var(--sub);flex:0 0 auto}
.dot.ok{background:var(--ok)}.dot.wait{background:var(--acc)}.dot.bad{background:var(--bad)}
.bal{font-size:21px;font-weight:650;font-variant-numeric:tabular-nums;margin:6px 0 2px}
.meta{font-size:13px;color:var(--sub)}
button{border:0;border-radius:10px;padding:9px 14px;font-size:14px;font-weight:600;background:var(--acc);color:#fff}
button.ghost{background:transparent;color:var(--acc);border:1px solid var(--acc)}
button:disabled{opacity:.45}
.bar{position:fixed;left:0;right:0;bottom:0;padding:10px 14px calc(10px + env(safe-area-inset-bottom));background:color-mix(in srgb,var(--card) 92%,transparent);border-top:1px solid var(--line);display:flex;gap:10px;backdrop-filter:saturate(180%) blur(12px)}
.bar button{flex:1}
.empty{color:var(--sub);text-align:center;padding:40px 10px;font-size:14px}
</style></head><body>
<header><h1>积分总览</h1><div class="sum">合计 <b id="total">—</b></div></header>
<div id="list"><div class="empty">加载中…</div></div>
<div class="bar">
  <button class="ghost" onclick="refresh()">刷新余额</button>
  <button id="all" onclick="checkinAll()">全部签到</button>
</div>
<script>
function el(t,c,x){const e=document.createElement(t);if(c)e.className=c;if(x!=null)e.textContent=x;return e}
async function j(u,o){const r=await fetch(u,o);return r.json()}
function fmt(n){return (n||0).toLocaleString(undefined,{maximumFractionDigits:2})}
async function refresh(){
  const list=document.getElementById('list');
  try{
    const rows=await j('/api/query');
    if(!Array.isArray(rows)||!rows.length){list.innerHTML='<div class="empty">还没有账号。请在壳层的设置里添加凭据。</div>';return}
    let total=0;list.innerHTML='';
    for(const r of rows){
      total+=r.balance||0;
      const c=el('div','card');
      const top=el('div','row');
      const nm=el('div','name');
      const dot=el('span','dot '+(r.error?'bad':(r.checked_in?'ok':'wait')));
      nm.appendChild(dot);nm.appendChild(el('span',null,(r.provider||'')+' · '+(r.account_id||'')));
      top.appendChild(nm);
      const btn=el('button','ghost',r.checked_in?'已签':'签到');
      btn.disabled=!!r.checked_in||!!r.error;
      btn.onclick=()=>checkinOne(r.account_id,btn);
      top.appendChild(btn);c.appendChild(top);
      c.appendChild(el('div','bal',(r.balance_text||fmt(r.balance))));
      const meta=r.error?('⚠ '+r.error):((r.detail||'')+(r.streak?(' · 第 '+r.streak+' 天'):''));
      c.appendChild(el('div','meta',meta));
      list.appendChild(c);
    }
    document.getElementById('total').textContent=fmt(total);
  }catch(e){list.innerHTML='<div class="empty">加载失败：'+e.message+'</div>'}
}
async function checkinOne(id,btn){
  btn.disabled=true;btn.textContent='…';
  try{const res=await j('/api/checkin?account='+encodeURIComponent(id),{method:'POST'});
    const r=Array.isArray(res)?res[0]:res;
    btn.textContent=r&&r.ok?(r.already?'已签':'完成'):'失败';
    await refresh();
  }catch(e){btn.textContent='失败';btn.disabled=false}
}
async function checkinAll(){
  const b=document.getElementById('all');b.disabled=true;b.textContent='签到中…';
  try{await j('/api/checkin',{method:'POST'});await refresh()}finally{b.disabled=false;b.textContent='全部签到'}
}
refresh();
</script></body></html>`
