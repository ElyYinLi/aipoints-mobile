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
		"function reloadThrottled()",
		"var RELOAD_KEY = 'last_reload_at';",
		"if (now - last < 60000) return false;",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("重载限流缺少 %q", want)
		}
	}
}

// 打开 App 必须执行「完整的一键签到」，而不是只查询或只签到。
//
// 这里钉住两个历史 bug：
//  1. todo 在 state.rows 还是空的时候就被组装 → 一个都不签
//     （用户：「打开 App 不签，点一键签到才签」）
//  2. 启动只走签到、不走领奖和抽奖 → 和点按钮的结果不一致
//     （用户：「打开 App 的是签到和领奖逻辑和一键签到功能一致」）
func TestIndexBootRunsFullCheckinFlow(t *testing.T) {
	src := indexHTML

	// 待签队列必须在 loadStates() 读完之后再组装
	if !strings.Contains(src, "function loadStates(){") {
		t.Fatal("缺少 loadStates()：启动时必须在拿到真实状态后再挑待签账号")
	}
	if !strings.Contains(src, "return loadStates()") {
		t.Fatal("checkinAll 没有先读状态，todo 会永远为空")
	}

	// 三件套一个都不能少
	for _, step := range []string{
		"post('/api/tasks/claim_all', {})",
		"post('/api/lottery/draw_all', {})",
	} {
		if !strings.Contains(src, step) {
			t.Fatalf("一键签到流程缺少步骤 %q", step)
		}
	}

	// 启动就走同一个 checkinAll，不能另写一条只签到的捷径
	if !strings.Contains(src, "autoTask = checkinAll({ quiet: true })") {
		t.Fatal("打开 App 没有复用 checkinAll，会与点按钮的行为不一致")
	}
}

// 只有「打开 App」才自动签到；切后台回来只刷新列表。
// 用户：「其他切后台等就不要自动签到和领奖了，这样容易黑号」。
func TestIndexResumeDoesNotCheckin(t *testing.T) {
	src := indexHTML

	if !strings.Contains(src, "function refreshOnResume(){") {
		t.Fatal("缺少 refreshOnResume()：回前台应只刷新不签到")
	}
	// 回前台路径里不能出现 POST 签到/领奖的调用
	start := strings.Index(src, "function refreshOnResume(){")
	if start < 0 {
		t.Fatal("找不到 refreshOnResume")
	}
	end := strings.Index(src[start:], "\n  }")
	body := src[start : start+end]
	for _, banned := range []string{"/api/checkin", "claim_all", "draw_all"} {
		if strings.Contains(body, banned) {
			t.Fatalf("回前台逻辑里出现了 %q，会把账号送进风控", banned)
		}
	}
	// visibilitychange / pageshow 必须走刷新路径
	if !strings.Contains(src, "resumeAfterLogin(); refreshOnResume();") {
		t.Fatal("visibilitychange/pageshow 未接到 refreshOnResume")
	}
	// 壳层入口只有两个明确的语义
	for _, want := range []string{"window.__pointsAutoCheckin = bootAuto;", "window.__pointsRefresh = refreshOnResume;"} {
		if !strings.Contains(src, want) {
			t.Fatalf("缺少壳层入口 %q", want)
		}
	}
	if strings.Contains(src, "resume_checkin") {
		t.Fatal("旧的 resume_checkin 续签路径应已删除（会导致回前台重复签到）")
	}
}

// 小额数值不能显示成「0」：灵石这类单位 0.04 被四舍五入成 0，看着像坏了。
func TestIndexSmallGainKeepsPrecision(t *testing.T) {
	src := indexHTML
	for _, want := range []string{
		"function fmtGain(v){",
		"function fmtMoney(n){",
		"Math.round(n * 100) / 100",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("小额数值格式化缺少 %q", want)
		}
	}
	// 奖励文案不能再直接用整数化的 fmt()
	if strings.Contains(src, "'，+' + fmt(r.gain)") {
		t.Fatal("奖励文案仍在用 fmt()，小额会被抹成 0")
	}
}

// 签到说明只写机制，不写背景故事（用户：「只写你的签到逻辑就行了」）。
func TestIndexHelpIsConcise(t *testing.T) {
	src := indexHTML
	start := strings.Index(src, "function openHelp(){")
	if start < 0 {
		t.Fatal("找不到 openHelp")
	}
	end := strings.Index(src[start:], "\n  /* ---------- 设置")
	body := src[start : start+end]
	if n := strings.Count(body, "para('"); n > 3 {
		t.Fatalf("签到说明有 %d 段，用户要求精简到三条", n)
	}
	for _, want := range []string{"打开 App 自动签一次", "切后台回来不会签", "只处理「未签」的账号"} {
		if !strings.Contains(body, want) {
			t.Fatalf("说明缺少关键结论 %q", want)
		}
	}
}
