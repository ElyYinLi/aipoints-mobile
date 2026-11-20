package panel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// taskMemo 记住「某账号今天已经成功领过哪些任务奖励」。
//
// 为什么必须有它：上游 daily_tasks 的 status 字段会滞后——刚领完仍在返回
// locked / 不返回 claimed，甚至在「已领取」的情况下返回 reward=0 的假成功。
// 只信上游会让同一天同一任务被反复申领、反复记账，重演 aipy-checkin v1.3.2
// 那次「今日灵石翻倍」的事故。
//
// 所以本地必须自己记一笔：key 是 <日期>|<provider>|<account>|<taskType>，
// 跨天自动失效（新 key 自然不命中），落盘保证重开 App 也记得住。
//
// 与 gainMemo 的区别：gainMemo 记「今天赚了多少」用于显示，taskMemo 记
// 「领过哪些」用于阻断请求。两者目标不同，不能合并。
type taskMemo struct {
	mu   sync.Mutex
	path string
	// data 里只保留今天的键，写入时顺手清理过期条目，文件不会越滚越大。
	data map[string]bool
}

// newTaskMemo 在 dir 下打开/创建 task_memo.json。dir 为空时只在内存里生效。
func newTaskMemo(dir string) *taskMemo {
	m := &taskMemo{data: map[string]bool{}}
	if dir == "" {
		return m
	}
	m.path = filepath.Join(dir, "task_memo.json")
	if b, err := os.ReadFile(m.path); err == nil {
		var raw map[string]bool
		if json.Unmarshal(b, &raw) == nil {
			today := time.Now().Format("2006-01-02")
			for k, v := range raw {
				// 只留今天的，避免旧日期条目无限堆积
				if v && len(k) > 10 && k[:10] == today {
					m.data[k] = true
				}
			}
		}
	}
	return m
}

// taskKey 生成去重键。
//
// 必须同时包含 provider 与 account：本引擎是多平台的，aipy 的
// "daily_ai_1" 与别的平台同名任务不能互相顶掉。
func taskKey(date, providerID, accountID, taskType string) string {
	return date + "|" + providerID + "|" + accountID + "|" + taskType
}

// seen 判断某键今天是否已领过。
func (m *taskMemo) seen(key string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[key]
}

// mark 记录一次成功领取并落盘。
func (m *taskMemo) mark(key string) {
	if m == nil || key == "" {
		return
	}
	m.mu.Lock()
	m.data[key] = true
	snapshot := make(map[string]bool, len(m.data))
	for k, v := range m.data {
		snapshot[k] = v
	}
	path := m.path
	m.mu.Unlock()

	if path == "" {
		return
	}
	if b, err := json.Marshal(snapshot); err == nil {
		atomicWrite(path, b)
	}
}

// clear 清空（面板「清空任务记录」用，主要给调试）。
func (m *taskMemo) clear() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.data = map[string]bool{}
	path := m.path
	m.mu.Unlock()
	if path != "" {
		atomicWrite(path, []byte("{}"))
	}
}

// atomicWrite 先写临时文件再 rename，避免断电留下半截 JSON。
// 权限 0600：里面是「哪天领了什么」，不含凭据，但仍不该被别的 App 读到。
func atomicWrite(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// taskMemoKey 是给调用方拼键的便捷函数（date 用当前本地日期）。
func taskMemoKey(providerID, accountID, taskType string) string {
	return taskKey(time.Now().Format("2006-01-02"), providerID, accountID, taskType)
}
