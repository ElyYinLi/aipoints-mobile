package panel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// gainMemo 记住「某账号今天签到拿了多少」。
//
// 为什么需要它：展示要的文案是「今日已签 +500 积分」，但平台接口不一定给这个数——
// 百度搭子的 loginBonusInfo 可能只回 hasIssued，WorkBuddy 的 today_credit 也时有时无。
// 而用户早上打开网页时积分就已经自动发了，我们连 POST 都没发过，更拿不到增量。
//
// 所以三种来源按优先级取：
//  1. 平台本次响应里明确给了今日积分（最可信）
//  2. 我们本次签到前后总额的差值（准确，但只在真的发过写请求时才有）
//  3. 本地记住的「上次算出来的今日值」（补上「没走我们这条链路」的情况）
//
// 第 3 条带日期校验，跨天自动失效，不会把昨天的数贴到今天。
type gainMemo struct {
	mu   sync.Mutex
	path string
	data map[string]memoEntry
}

type memoEntry struct {
	Date string  `json:"date"` // 2006-01-02
	Gain float64 `json:"gain"`
}

func newGainMemo(dir string) *gainMemo {
	m := &gainMemo{path: filepath.Join(dir, "gain_memo.json"), data: map[string]memoEntry{}}
	if b, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(b, &m.data)
	}
	if m.data == nil {
		m.data = map[string]memoEntry{}
	}
	return m
}

func memoKey(providerID, accountID string) string { return providerID + "|" + accountID }

// Set 记录今天该账号的签到收益。gain<=0 时清掉（避免显示 0 或负数）。
func (m *gainMemo) Set(providerID, accountID string, gain float64) {
	if m == nil {
		return
	}
	k := memoKey(providerID, accountID)
	today := time.Now().Format("2006-01-02")
	m.mu.Lock()
	if gain <= 0 {
		delete(m.data, k)
	} else {
		m.data[k] = memoEntry{Date: today, Gain: gain}
	}
	snapshot := make(map[string]memoEntry, len(m.data))
	for kk, vv := range m.data {
		// 顺手清掉过期条目，文件不会越积越大
		if vv.Date == today {
			snapshot[kk] = vv
		}
	}
	m.mu.Unlock()

	b, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	_ = os.WriteFile(m.path, b, 0o600)
}

// Get 取今天该账号的签到收益，不是今天的返回 0。
func (m *gainMemo) Get(providerID, accountID string) float64 {
	if m == nil {
		return 0
	}
	today := time.Now().Format("2006-01-02")
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.data[memoKey(providerID, accountID)]
	if !ok || e.Date != today {
		return 0
	}
	return e.Gain
}
