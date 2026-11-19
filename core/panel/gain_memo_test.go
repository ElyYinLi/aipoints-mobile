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
