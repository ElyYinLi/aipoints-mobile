package panel

import (
	"strings"
	"testing"
)

// 「已签」状态行的显示规则（用户为此反馈过四次，最后一次是「+？很搞笑」）：
//   - 有具体数值 → 今日已签 +N 单位
//   - 没有数值   → 今日已签          （干净收尾，绝不出现 +? 这种半成品）
//
// 这条规则只存在于模板 JS 里，Go 编译器检查不到，所以用字符串断言钉住。
func TestIndexDetailTextGainRule(t *testing.T) {
	src := indexHTML

	want := []struct{ name, snippet string }{
		{"有数值时拼单位", "return g ? ('今日已签 +' + g + ' ' + unit) : '今日已签';"},
		{"数值统一取值", "function gainNum(r){"},
		{"未签可得也带单位", "return '今日未签 · 可得 +' + g2 + ' ' + unit;"},
	}
	for _, c := range want {
		if !strings.Contains(src, c.snippet) {
			t.Fatalf("%s：模板里缺少 %q", c.name, c.snippet)
		}
	}
	// 反面断言：`+?` 这种占位必须彻底消失（用户明确说「很搞笑」）
	if strings.Contains(src, "(g || '?')") {
		t.Fatal("detailText 里仍残留 +? 占位，拿不到数值时应只写「今日已签」")
	}
}

// 「跟随系统」主题已从产品中移除（用户：「你都实现不了，要它有什么用」）。
// 这里做三重保证：选项没了、系统探测代码没了、历史值不会把界面卡在怪状态。
func TestIndexThemeHasNoSystemMode(t *testing.T) {
	src := indexHTML

	// 只剩两个选项
	if !strings.Contains(src, "var THEMES = ['light', 'dark'];") {
		t.Fatal("THEMES 应只有 light / dark")
	}
	// 系统探测的三套机制必须全部消失，避免留死代码
	for _, dead := range []string{
		"window.__pointsOnSystemTheme",
		"__pointsSystemDark",
		"prefers-color-scheme: dark",
	} {
		if strings.Contains(src, dead) {
			t.Fatalf("「跟随系统」相关代码仍在：%q", dead)
		}
	}
	// 历史值 'system' 必须落到默认深色，不能停在无对应主题的状态
	if !strings.Contains(src, "var THEME_DEFAULT = 'dark';") {
		t.Fatal("缺少默认主题常量（历史值 system 需落到它）")
	}
}

// 签到必须逐个账号进行并把进度写进顶部活动条。
// 之前是「一次 POST 让后端批量签」，中途没有任何反馈，用户完全看不出
// 「谁在签、签到第几个、签完没」，于是怀疑软件没在干活。
func TestIndexCheckinShowsProgress(t *testing.T) {
	src := indexHTML
	for _, want := range []string{
		"正在签到…（' + n + '/' + total + '）", // 第几个 / 共几个
		"function actShow(",              // 活动条：进行中
		"function actDone(",              // 活动条：完成
		"function actErr(",               // 活动条：失败
		"checkinAll({ quiet: true })",    // 自动签到也走同一套进度
		"正在刷新列表…",                        // 明确告知「签完还要刷新」
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("签到进度反馈缺少 %q", want)
		}
	}
	// 反面断言：不能再退回「一次批量 POST」——那样拿不到任何中间进度
	if strings.Contains(src, "j('/api/checkin', { method: 'POST' })") {
		t.Fatal("仍在使用批量签到接口，前端拿不到逐账号进度")
	}
}

// iOS/Android 后台久了会被系统回收 WebView 渲染进程，回来时页面是死的。
// 必须「整页重建」，光调 JS 没用。这条逻辑同时存在于页面侧（限流 reload）。
func TestIndexReloadGuardExists(t *testing.T) {
	src := indexHTML
	for _, want := range []string{
		"function notifyReloadThrottled()",
		"var RELOAD_KEY = 'last_reload_at';",
		"if (now - last < 60000) return false;",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("重载限流缺少 %q", want)
		}
	}
}
