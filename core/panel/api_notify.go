package panel

import (
	"net/http"
)

// handleNotify 接收前端算好的签到汇总并推送到 Webhook。
//
// POST /api/notify {"trigger":"manual","accounts":[...],"summary":"..."}
//
// 为什么由前端把汇总传上来：签到、领任务、抽奖这三段在 JS 里串行执行，
// 「新签 3，已签 1，失败 1｜任务 2 个 +50 灵石」这句话只有 JS 算得出来。
// Go 侧接收后只负责「渲染 + 发送 + 脱敏」。
func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p NotifyPayload
	if err := readBody(r, &p); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if p.Trigger == "" {
		p.Trigger = "manual"
	}
	if s.notify == nil {
		writeJSON(w, map[string]any{"ok": true, "sent": false, "reason": "notifier 未就绪"})
		return
	}
	// 推送失败绝不影响签到结果：这里一律返回 ok=true，只带 sent 与 err 供前端记日志。
	//
	// sent 有三态，不能只看 err == nil：
	//   err == nil        → sent:true   真的发出去了
	//   按配置跳过         → sent:false, skipped:true   用户自己关的，不算故障
	//   真失败            → sent:false, error:"..."     地址错了/网络不通
	err := s.notify.send(r.Context(), p, false)
	out := map[string]any{"ok": true, "sent": err == nil}
	if err != nil {
		out["error"] = err.Error()
		if isSkip(err) {
			out["skipped"] = true
		}
	}
	writeJSON(w, out)
}

// handleNotifyConfig 读/写通知配置。
//
// GET  /api/notify/config  → {ok, config, types, fail_streak, last_error, disabled}
// POST /api/notify/config  {enabled,type,url,on_success,on_failure,on_manual,on_start}
func (s *Server) handleNotifyConfig(w http.ResponseWriter, r *http.Request) {
	if s.notify == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "通知模块未就绪"})
		return
	}
	if r.Method == http.MethodGet {
		st := s.notify.status()
		st["ok"] = true
		writeJSON(w, st)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var c NotifyConfig
	if err := readBody(r, &c); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := s.notify.save(c); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "config": s.notify.config()})
}

// handleNotifyTest 发送一条测试消息。
//
// 这个入口必须有：源项目（aipy-checkin）没有，用户把 Webhook 地址填错
// 只能等第二天签到失败才发现。测试走与正式推送完全相同的代码路径，
// 所以「测试通过 = 正式也通」。
func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.notify == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "通知模块未就绪"})
		return
	}
	cfg := s.notify.config()
	if cfg.URL == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "请先填写 Webhook 地址"})
		return
	}
	p := NotifyPayload{
		Trigger: "manual",
		Accounts: []NotifyAccountLine{
			{Provider: "aipy", Label: "测试账号 A", Status: "ok", Message: "签到成功", Gain: 100, Unit: "灵石"},
			{Provider: "dumate", Label: "测试账号 B", Status: "already", Message: "今日已签"},
			{Provider: "trae", Label: "测试账号 C", Status: "fail", Message: "登录已失效，请重新登录"},
		},
		Summary: "这是一条测试消息，收到就说明配置正确。",
	}
	if err := s.notify.send(r.Context(), p, true); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleNotifyReset 清掉「已自动停用」标记（用户换了地址后手动恢复）。
func (s *Server) handleNotifyReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.notify == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "通知模块未就绪"})
		return
	}
	c := s.notify.config()
	c.Enabled = true
	if err := s.notify.save(c); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
