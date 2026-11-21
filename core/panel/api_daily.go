package panel

import (
	"math/rand"
	"net/http"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// 今日状态接口（0.6.4）
//
// 用户对启动逻辑的原话：
//
//	「软件打开的签到逻辑直接改成：探测今天是否有签到日志，有就只刷新数据
//	 并提示有哪几个没签到、哪些奖励没领，没有签到日志直接走一遍一键签到。
//	 容易黑号知道吗。」
//
// 于是「今天跑过没有」这件事必须由引擎侧说了算（落盘，进程被杀也不丢），
// 而不是前端凭内存猜。前端只问一句 /api/daily，然后二选一。
//
// 注意边界：**一键签到按钮不受这里任何影响**。按钮每次都真的跑一轮，
// 这里只负责回答「今天自动签到该不该跑」。

// handleDaily 返回今天的执行记录。
//
// GET /api/daily
// → {"ok":true,"ran":true,"round":"...","at":"08:12:40","done":true,
//
//	"trigger":"start","accounts":{"aipy|u1":"ok", ...}}
//
// ran=true 表示「今天已经自动跑过一轮完整流程」，前端据此只刷新不重跑。
// done=false（有记录但没走完，例如中途被杀）时 ran 依然为 false：
// 宁可补跑一次，也不要出现「点了启动但整天不签」。
func (s *Server) handleDaily(w http.ResponseWriter, r *http.Request) {
	e, ok := s.daily.Today()
	ran := ok && e.Done
	out := map[string]any{
		"ok": true, "ran": ran, "date": todayKey(),
	}
	if ok {
		out["round"] = e.Round
		out["at"] = e.At
		out["done"] = e.Done
		out["trigger"] = e.Trigger
		out["accounts"] = e.Accounts
	}
	writeJSON(w, out)
}

// handleDailyMark 让前端在流程的关键节点打点。
//
// POST /api/daily/mark {"phase":"begin","round":"...","trigger":"start"}
// POST /api/daily/mark {"phase":"account","round":"...","provider":"aipy","account":"u1","status":"ok"}
// POST /api/daily/mark {"phase":"finish","round":"..."}
//
// 为什么由前端打点：签到、领奖励、抽奖三段是前端串行驱动的（逐个账号 POST），
// 引擎只知道单次请求，不知道「这一轮什么时候算完」。由前端在真正结束时打一个
// finish，才不会出现「签到跑了一半就被判定为已完成」。
func (s *Server) handleDailyMark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Phase    string `json:"phase"`
		Round    string `json:"round"`
		Trigger  string `json:"trigger"`
		Provider string `json:"provider"`
		Account  string `json:"account"`
		Status   string `json:"status"`
	}
	if err := readBody(r, &in); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	switch in.Phase {
	case "begin":
		round := in.Round
		if round == "" {
			round = NewRoundID()
		}
		trig := in.Trigger
		if trig == "" {
			trig = "start"
		}
		s.daily.Begin(round, trig)
		httpx.AddLog(httpx.LogEntry{
			Level: "info", Scope: "app", Round: round,
			Msg: "开始一轮一键签到（来源：" + trigName(trig) + "）",
		})
		writeJSON(w, map[string]any{"ok": true, "round": round})
	case "account":
		if in.Provider == "" || in.Account == "" || in.Status == "" {
			writeJSON(w, map[string]any{"ok": false, "error": "缺少 provider/account/status"})
			return
		}
		s.daily.SetAccount(in.Provider, in.Account, in.Status)
		writeJSON(w, map[string]any{"ok": true})
	case "finish":
		s.daily.Finish()
		httpx.AddLog(httpx.LogEntry{
			Level: "info", Scope: "app", Round: in.Round,
			Msg: "本轮结束，今日不再自动签到（再开 App 只刷新）",
		})
		writeJSON(w, map[string]any{"ok": true})
	default:
		writeJSON(w, map[string]any{"ok": false, "error": "未知 phase：" + in.Phase})
	}
}

func trigName(t string) string {
	switch t {
	case "start":
		return "打开 App"
	case "manual":
		return "手动一键签到"
	}
	return t
}

// NewRoundID 生成一个 4 位十六进制短 id，给日志用。
//
// 用途：一轮一键签到会打十几条日志，用户把它们贴过来时，
// 靠 round 一眼就能分清「哪些是这一轮的、哪些是上一次的」。
// 4 位够用（同一天几十轮也不会撞），窗口里只显示 4 字符不占地方。
func NewRoundID() string {
	const hex = "0123456789abcdef"
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, 4)
	for i := range b {
		b[i] = hex[r.Intn(len(hex))]
	}
	return string(b)
}
