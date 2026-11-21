package panel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// dailyMemo 记住「今天这一轮一键签到跑过没有、每个账号结果如何」。
//
// 为什么需要它（0.6.4 的启动逻辑）：
//
//	用户明确要求：「软件打开时先探测今天有没有签到日志，有就只刷新数据并提示
//	哪几个没签到、哪些奖励没领；没有才走一遍一键签到」。
//	理由是反复自动签到会把账号送进风控（他的原话是「容易黑号」）。
//
// 所以「今天跑过没有」必须落盘：进程会被系统随时杀掉，内存变量留不住，
// 而用户一天可能开十次 App。落盘之后，第二次、第三次打开都只刷新不重跑。
//
// 记录什么：
//   - Date：只有等于今天才算「今天跑过」，跨天自动失效；
//   - Round：这一轮的短 id，和日志里的 round 字段对应，方便对时间线；
//   - At：这一轮结束（或被判定完成）的时刻；
//   - Done：这轮是否走完了完整流程（签到 + 领奖 + 抽奖）。
//     中途被杀时 Done=false，下一次打开会允许补跑一次，不会因为
//     「点了启动但没跑完」而整天都不签。
//   - Accounts：逐账号的结果（status: ok / already / fail）。
//     用于「今天跑过」分支里提示「哪几个没签到」——失败的那几个不会再自动重试
//     （用户拍板：当天不再自动重试，只在提示里列出来）。
//
// 一键签到按钮**完全不受它影响**：按钮每次点都真的重跑一轮，这里只写不读。
type dailyMemo struct {
	mu   sync.Mutex
	path string
	data map[string]dailyEntry // key 为日期 2006-01-02
}

type dailyEntry struct {
	Date     string            `json:"date"`
	Round    string            `json:"round"`
	At       string            `json:"at"` // HH:MM:SS
	Done     bool              `json:"done"`
	Trigger  string            `json:"trigger"` // start / manual
	Accounts map[string]string `json:"accounts"`
}

func newDailyMemo(dir string) *dailyMemo {
	m := &dailyMemo{data: map[string]dailyEntry{}}
	if dir == "" {
		return m
	}
	m.path = filepath.Join(dir, "daily_memo.json")
	if b, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(b, &m.data)
	}
	if m.data == nil {
		m.data = map[string]dailyEntry{}
	}
	return m
}

func todayKey() string { return time.Now().Format("2006-01-02") }

// Begin 在一轮一键签到开始时打点。
//
// 立刻落盘（Done=false）：如果这一轮因为切后台被系统掐死，下次打开看到
// Done=false 就会允许补跑，而不是整天不签。
func (m *dailyMemo) Begin(round, trigger string) {
	if m == nil {
		return
	}
	day := todayKey()
	m.mu.Lock()
	m.data[day] = dailyEntry{
		Date: day, Round: round, At: time.Now().Format("15:04:05"),
		Done: false, Trigger: trigger, Accounts: map[string]string{},
	}
	m.mu.Unlock()
	m.persist()
}

// SetAccount 记录单个账号在这一轮的结果（ok / already / fail）。
func (m *dailyMemo) SetAccount(providerID, accountID, status string) {
	if m == nil {
		return
	}
	day := todayKey()
	k := memoKey(providerID, accountID)
	m.mu.Lock()
	e, ok := m.data[day]
	if !ok {
		e = dailyEntry{Date: day, Accounts: map[string]string{}}
	}
	if e.Accounts == nil {
		e.Accounts = map[string]string{}
	}
	e.Accounts[k] = status
	m.data[day] = e
	m.mu.Unlock()
	m.persist()
}

// Finish 在一轮走完时打点（含领奖与抽奖）。
func (m *dailyMemo) Finish() {
	if m == nil {
		return
	}
	day := todayKey()
	m.mu.Lock()
	e := m.data[day]
	e.Date, e.Done, e.At = day, true, time.Now().Format("15:04:05")
	m.data[day] = e
	m.mu.Unlock()
	m.persist()
}

// Today 返回今天的记录。没有记录时 ok=false。
func (m *dailyMemo) Today() (dailyEntry, bool) {
	if m == nil {
		return dailyEntry{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.data[todayKey()]
	if !ok || e.Date != todayKey() {
		return dailyEntry{}, false
	}
	return e, true
}

func (m *dailyMemo) persist() {
	if m == nil || m.path == "" {
		return
	}
	m.mu.Lock()
	// 只留最近三天：文件不会越积越大，也不至于跨零点就把刚跑的记录丢了
	cut := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	snap := make(map[string]dailyEntry, len(m.data))
	for k, v := range m.data {
		if k >= cut {
			snap[k] = v
		}
	}
	m.mu.Unlock()
	b, err := json.Marshal(snap)
	if err != nil {
		return
	}
	_ = os.WriteFile(m.path, b, 0o600)
}
