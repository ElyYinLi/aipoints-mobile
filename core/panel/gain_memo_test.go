package panel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestGainMemoRoundTrip 记录 / 读取 / 跨天失效。
func TestGainMemoRoundTrip(t *testing.T) {
	m := newGainMemo(t.TempDir())
	m.Set("dumate", "a1", 50)
	if got := m.Get("dumate", "a1"); got != 50 {
		t.Fatalf("期望 50，实际 %v", got)
	}
	// 换个账号不能串
	if got := m.Get("dumate", "a2"); got != 0 {
		t.Fatalf("未记录的账号应为 0，实际 %v", got)
	}
	// gain<=0 视为「清掉」，避免显示 0 或负数
	m.Set("dumate", "a1", 0)
	if got := m.Get("dumate", "a1"); got != 0 {
		t.Fatalf("清零后应为 0，实际 %v", got)
	}
}

// TestGainMemoExpiresAcrossDay 昨天的值不能贴到今天。
func TestGainMemoExpiresAcrossDay(t *testing.T) {
	m := newGainMemo(t.TempDir())
	// 手写一条昨天的记录，绕过 Set 的「只写今天」
	m.data["dumate|a1"] = memoEntry{Date: time.Now().AddDate(0, 0, -1).Format("2006-01-02"), Gain: 999}
	if got := m.Get("dumate", "a1"); got != 0 {
		t.Fatalf("跨天应失效，实际 %v", got)
	}
}

// TestGainMemoPersists 重启后仍能读到（落盘生效）。
func TestGainMemoPersists(t *testing.T) {
	dir := t.TempDir()
	newGainMemo(dir).Set("workbuddy", "b1", 100)
	if _, err := os.Stat(filepath.Join(dir, "gain_memo.json")); err != nil {
		t.Fatalf("应落盘: %v", err)
	}
	again := newGainMemo(dir)
	if got := again.Get("workbuddy", "b1"); got != 100 {
		t.Fatalf("重启后应读到 100，实际 %v", got)
	}
}

// TestGainMemoFreshFlag 「本次进程签的」和「磁盘读回来的」必须能区分。
//
// 这条区分决定了能不能纠正平台状态：
//   - fresh 的（本次进程亲手签下）→ 可以纠正 WorkBuddy 那种「签完仍报未签」
//   - 磁盘恢复的 → 只能补数值，不能改状态，否则平台说未签也会被改成已签，
//     结果是前端认为不用签，永远不去签到（0.5.4 修的真实 bug）
func TestGainMemoFreshFlag(t *testing.T) {
	dir := t.TempDir()
	m := newGainMemo(dir)
	m.Set("workbuddy", "b1", 100)
	if !m.Fresh("workbuddy", "b1") {
		t.Fatal("刚 Set 的条目必须是 fresh")
	}

	// 模拟重启：新实例从磁盘读到同一条，但不再是本次进程签的
	again := newGainMemo(dir)
	if got := again.Get("workbuddy", "b1"); got != 100 {
		t.Fatalf("数值应保留，实际 %v", got)
	}
	if again.Fresh("workbuddy", "b1") {
		t.Fatal("磁盘恢复的条目不能标记为 fresh——会误改平台状态")
	}

	// 清零时 fresh 也要一起清掉，避免残留标记
	m.Set("workbuddy", "b1", 0)
	if m.Fresh("workbuddy", "b1") {
		t.Fatal("清零后不应还是 fresh")
	}
}
