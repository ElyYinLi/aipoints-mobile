package panel

import (
	"context"
	"net/http"
	"strings"
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

// handleTaskClaimAll 一键领取全部启用账号的可领任务。
//
// POST /api/tasks/claim_all
// 逐个账号：先查任务列表，对 claimable 的逐条领取。单个任务失败不影响其余，
// 写请求之间留间隔（同设备批量写最容易触发风控）。
func (s *Server) handleTaskClaimAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	type claimRow struct {
		Provider  string  `json:"provider"`
		AccountID string  `json:"account_id"`
		Claimed   int     `json:"claimed"`
		Gain      float64 `json:"gain"`
		Unit      string  `json:"unit,omitempty"`
		Error     string  `json:"error,omitempty"`
	}
	out := make([]claimRow, 0, len(list))
	for _, a := range list {
		if !a.Enabled {
			continue
		}
		p, ok := s.opt.Reg.Get(a.Provider)
		if !ok {
			continue
		}
		claimer, ok := p.(provider.TaskClaimer)
		if !ok {
			// 该平台没有任务体系，直接跳过
			continue
		}
		cred, err := decodeCred(a)
		if err != nil {
			out = append(out, claimRow{Provider: a.Provider, AccountID: a.AccountID, Error: "凭据损坏"})
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		tasks, terr := claimer.Tasks(ctx, cred)
		cancel()
		if terr != nil {
			out = append(out, claimRow{Provider: a.Provider, AccountID: a.AccountID, Error: terr.Error()})
			continue
		}
		row := claimRow{Provider: a.Provider, AccountID: a.AccountID}
		var errs []string
		for _, t := range tasks {
			if !t.Claimable {
				continue
			}
			ctx2, cancel2 := context.WithTimeout(r.Context(), 30*time.Second)
			res, cerr := claimer.ClaimTask(ctx2, cred, t.TaskType)
			cancel2()
			if cerr != nil {
				res.Error = cerr.Error()
			}
			if res.OK {
				row.Claimed++
				row.Gain += res.Gain
				if row.Unit == "" && res.Unit != "" {
					row.Unit = res.Unit
				}
			} else if res.Error != "" {
				errs = append(errs, res.Error)
			}
			// 写请求间隔：同设备批量领取最容易触发风控
			time.Sleep(800 * time.Millisecond)
		}
		if len(errs) > 0 {
			row.Error = strings.Join(errs, "；")
		}
		out = append(out, row)
	}
	writeJSON(w, map[string]any{"ok": true, "results": out})
}
