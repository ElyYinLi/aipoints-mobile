package scheduler

import (
	"testing"
	"time"
)

func TestDailyStableAndSpaced(t *testing.T) {
	day := time.Date(2026, 11, 18, 0, 0, 0, 0, time.Local)
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.Local)
	end := start.Add(150 * time.Minute)
	accounts := []string{"a1", "a2", "a3", "a4"}

	p1 := Daily(day, "workbuddy", accounts, start, end)
	p2 := Daily(day, "workbuddy", accounts, start, end)
	if len(p1) != len(accounts) {
		t.Fatalf("期望 %d 条排期，实际 %d", len(accounts), len(p1))
	}
	// 同一天同样输入必须稳定（用户重开 App 时间不能跳）
	for i := range p1 {
		if !p1[i].At.Equal(p2[i].At) {
			t.Fatalf("排期不稳定: %v vs %v", p1[i].At, p2[i].At)
		}
	}
	// 相邻间隔 >= MinGap
	for i := 1; i < len(p1); i++ {
		gap := p1[i].At.Sub(p1[i-1].At)
		if gap < MinGap {
			t.Fatalf("间隔不足: %v < %v", gap, MinGap)
		}
	}
	// 除越界挤压外，都应落在窗口内
	for _, p := range p1 {
		if p.At.Before(start) {
			t.Fatalf("排期早于窗口: %v", p.At)
		}
	}
}

func TestDailyEmpty(t *testing.T) {
	if got := Daily(time.Now(), "x", nil, time.Now(), time.Now().Add(time.Hour)); got != nil {
		t.Fatal("空账号应返回 nil")
	}
}

func TestDue(t *testing.T) {
	now := time.Now()
	plans := []Plan{
		{Provider: "a", AccountID: "1", At: now.Add(-time.Minute)},
		{Provider: "a", AccountID: "2", At: now.Add(time.Hour)},
	}
	got := Due(plans, now, func(p Plan) bool { return p.AccountID == "1" })
	if len(got) != 0 {
		t.Fatalf("已完成的应被过滤，实际 %d", len(got))
	}
	got = Due(plans, now, nil)
	if len(got) != 1 || got[0].AccountID != "1" {
		t.Fatalf("应只返回到点的 1 条，实际 %+v", got)
	}
}
