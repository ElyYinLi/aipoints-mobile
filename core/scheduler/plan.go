// Package scheduler 复刻桌面版的「随机签到」排期：
// 同一平台下多个账号在给定时间窗内各排一个时刻，彼此至少间隔 MinGap，
// 且同一 (日期, 平台, 账号) 每次计算结果稳定（用户重开 App 不会看到时间乱跳）。
package scheduler

import (
	"hash/fnv"
	"sort"
	"time"
)

// Plan 是一个账号当天的执行时刻。
type Plan struct {
	Provider  string    `json:"provider"`
	AccountID string    `json:"account_id"`
	At        time.Time `json:"at"`
}

// MinGap 是同平台账号之间的最小间隔，避免同设备批量请求触发风控。
const MinGap = 10 * time.Minute

// Daily 为一批账号生成当天的排期。
//
// 算法：
//  1. 每个账号用 FNV(date|provider|account) 算一个稳定的伪随机偏移，落在窗口内；
//  2. 按时刻排序后做一次「向后挤压」，保证相邻间隔 >= MinGap；
//  3. 挤压可能越过窗口末尾 —— 越界的顺延到窗口最后一段（宁可晚，不要并发）。
func Daily(date time.Time, providerID string, accounts []string, start, end time.Time) []Plan {
	if len(accounts) == 0 {
		return nil
	}
	window := end.Sub(start)
	if window <= 0 {
		return nil
	}
	day := date.Format("2006-01-02")

	type item struct {
		id  string
		off time.Duration
	}
	items := make([]item, 0, len(accounts))
	for _, id := range accounts {
		h := fnv.New64a()
		h.Write([]byte(day + "|" + providerID + "|" + id))
		// 取模到窗口内；窗口太小时退化为等差排布
		off := time.Duration(h.Sum64() % uint64(window))
		items = append(items, item{id: id, off: off})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].off < items[j].off })

	plans := make([]Plan, 0, len(items))
	var last time.Time
	for i, it := range items {
		at := start.Add(it.off)
		if i > 0 && at.Sub(last) < MinGap {
			at = last.Add(MinGap)
		}
		if at.After(end) {
			// 越界：贴到窗口尾部，若仍冲突则退化为窗口末段均分
			at = end.Add(-MinGap * time.Duration(len(items)-1-i))
			if at.Before(last) {
				at = last.Add(MinGap)
			}
		}
		plans = append(plans, Plan{Provider: providerID, AccountID: it.id, At: at})
		last = at
	}
	return plans
}

// Due 返回在 now 时刻应该执行的排期（含已过点但未执行的补签窗口）。
//
// 桌面版的做法是「错过就补签」，这里保持一致：只要 at <= now 且未执行过就返回。
func Due(plans []Plan, now time.Time, done func(Plan) bool) []Plan {
	var out []Plan
	for _, p := range plans {
		if p.At.After(now) {
			continue
		}
		if done != nil && done(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}
