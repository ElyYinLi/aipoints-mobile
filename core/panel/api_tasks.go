package panel

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
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
		Account    string `json:"account"`
		TaskType   string `json:"task_type"`
		Provider   string `json:"provider"`
		IgnoreMemo bool   `json:"ignore_memo"`
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
		// 本地去重：今天已经成功领过这一项就直接返回，不打上游。
		// 上游 status 会滞后，只靠它挡不住重复申领（见 task_memo.go 注释）。
		key := taskMemoKey(a.Provider, a.AccountID, in.TaskType)
		if !in.IgnoreMemo && s.tasks != nil && s.tasks.seen(key) {
			writeJSON(w, map[string]any{
				"provider": a.Provider, "account_id": a.AccountID,
				"ok": false, "already": true, "deduped": true,
				"message": "今日已领取过该任务奖励",
			})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		res, terr := claimer.ClaimTask(ctx, cred, in.TaskType)
		if terr != nil {
			res.Error = terr.Error()
			res.Kind = provider.AsAPIError(terr).Kind
		}
		if res.Provider == "" {
			res.Provider = a.Provider
		}
		if res.AccountID == "" {
			res.AccountID = a.AccountID
		}
		// 只有真拿到奖励才记账。上游返回 0 视为没领到（多半已领过或状态已变），
		// 记下来会让用户以为到账——这是 aipy-checkin v1.3.3 修掉的假账。
		if res.OK && res.Gain > 0 {
			if s.tasks != nil {
				s.tasks.mark(key)
			}
			httpx.AddLog(httpx.LogEntry{
				Level: "info", Scope: "task",
				Msg: fmt.Sprintf("领取任务奖励 %s/%s %s +%s %s",
					a.Provider, a.AccountID, in.TaskType, fmtGainNum(res.Gain), res.Unit),
			})
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
	httpx.AddLog(httpx.LogEntry{Level: "info", Scope: "task", Step: "领任务奖励", Msg: "开始扫描可领任务"})
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
		step := "领任务 " + s.acctLabel(a)
		t0 := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		tasks, terr := claimer.Tasks(ctx, cred)
		cancel()
		if terr != nil {
			httpx.AddLog(httpx.LogEntry{
				Level: "warn", Scope: a.Provider, Step: step,
				CostMS: time.Since(t0).Milliseconds(), Msg: "任务列表读取失败：" + terr.Error(),
			})
			out = append(out, claimRow{Provider: a.Provider, AccountID: a.AccountID, Error: terr.Error()})
			continue
		}
		row := claimRow{Provider: a.Provider, AccountID: a.AccountID}
		var errs []string
		claimable := 0
		for _, t := range tasks {
			if !t.Claimable {
				continue
			}
			claimable++
			ctx2, cancel2 := context.WithTimeout(r.Context(), 30*time.Second)
			t1 := time.Now()
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
				httpx.AddLog(httpx.LogEntry{
					Level: "info", Scope: a.Provider, Step: step,
					CostMS: time.Since(t1).Milliseconds(),
					Msg:    "领取成功：" + t.Title + gainLogTail(provider.Result{Gain: res.Gain, Unit: res.Unit}),
				})
			} else {
				httpx.AddLog(httpx.LogEntry{
					Level: "warn", Scope: a.Provider, Step: step,
					CostMS: time.Since(t1).Milliseconds(),
					Msg:    "领取失败：" + t.Title + "（" + orDefault(res.Error, "平台未返回原因") + "）",
				})
				if res.Error != "" {
					errs = append(errs, res.Error)
				}
			}
			// 写请求间隔：同设备批量领取最容易触发风控
			time.Sleep(800 * time.Millisecond)
		}
		// 一个可领的都没有也要留一行：「本轮没有任务需要领」本身就是结论，
		// 否则用户会怀疑这步是不是根本没跑（0.6.4 加了这行）。
		if claimable == 0 {
			httpx.AddLog(httpx.LogEntry{
				Level: "info", Scope: a.Provider, Step: step,
				CostMS: time.Since(t0).Milliseconds(), Msg: "没有可领的任务",
			})
		}
		if len(errs) > 0 {
			row.Error = strings.Join(errs, "；")
		}
		out = append(out, row)
	}
	writeJSON(w, map[string]any{"ok": true, "results": out})
}

// orDefault 空串时返回兜底文案。
func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// fmtGainNum 去掉无意义的小数位：1.5 → "1.5"，2 → "2"，0.04 → "0.04"。
// 日志里直接 %v 会打出 0.04000000000000001 这种浮点噪声。
func fmtGainNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
