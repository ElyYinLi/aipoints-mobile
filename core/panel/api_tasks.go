package panel

import (
	"context"
	"net/http"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/provider"
)

// handleTasks 查询指定账号的每日任务（仅支持 TaskClaimer 平台）。
//
// GET /api/tasks?account=<account_id>
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	accountID := r.URL.Query().Get("account")
	if accountID == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "缺少 account"})
		return
	}
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for _, a := range list {
		if a.AccountID != accountID {
			continue
		}
		p, ok := s.opt.Reg.Get(a.Provider)
		if !ok {
			writeJSON(w, map[string]any{"ok": false, "error": "未支持的平台"})
			return
		}
		claimer, ok := p.(provider.TaskClaimer)
		if !ok {
			writeJSON(w, map[string]any{"ok": false, "error": "该平台不支持任务领取", "tasks": []provider.Task{}})
			return
		}
		cred, err := decodeCred(a)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "凭据损坏"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		tasks, terr := claimer.Tasks(ctx, cred)
		if terr != nil {
			writeJSON(w, map[string]any{"ok": false, "error": terr.Error(), "tasks": []provider.Task{}})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "provider": a.Provider, "tasks": tasks})
		return
	}
	writeJSON(w, map[string]any{"ok": false, "error": "账号不存在"})
}

// handleTaskClaim 领取指定账号的某个每日任务奖励。
//
// POST /api/tasks/claim {"account":"...","task_type":"..."}
func (s *Server) handleTaskClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireStore(w) {
		return
	}
	var in struct {
		Account  string `json:"account"`
		TaskType string `json:"task_type"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if in.Account == "" || in.TaskType == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "缺少 account / task_type"})
		return
	}
	list, err := s.opt.Store.List()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for _, a := range list {
		if a.AccountID != in.Account {
			continue
		}
		p, ok := s.opt.Reg.Get(a.Provider)
		if !ok {
			writeJSON(w, map[string]any{"ok": false, "error": "未支持的平台"})
			return
		}
		claimer, ok := p.(provider.TaskClaimer)
		if !ok {
			writeJSON(w, map[string]any{"ok": false, "error": "该平台不支持任务领取"})
			return
		}
		cred, err := decodeCred(a)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "凭据损坏"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		res, terr := claimer.ClaimTask(ctx, cred, in.TaskType)
		if terr != nil {
			res.Error = terr.Error()
		}
		writeJSON(w, res)
		return
	}
	writeJSON(w, map[string]any{"ok": false, "error": "账号不存在"})
}
