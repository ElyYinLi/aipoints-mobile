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

// 0.6.4：页面侧的「整页 reload 兜底」已按用户要求全部删除。
//
// 用户原话：「后台久了回前台『读取失败』这个问题你现有的措施全部删掉……
// 都没卵用……实质上什么都没改变」「删掉这些措施，我不想说第三遍」。
//
// 真因是 Go 侧复用了系统已回收的 keep-alive 连接，已在 httpx.New 里
// 改成 DisableKeepAlives（有单测钉住）。页面侧因此不该再有任何
// reload/重建/自动重试的逻辑——那只会把一个错误变成两个错误。
func TestIndexNoReloadFallback(t *testing.T) {
	src := indexHTML
	for _, dead := range []string{
		"function reloadThrottled()",
		"RELOAD_KEY",
		"reloadFromBlank", // 页面不得自己重建
		"notifyReload",    // 页面不得通知壳层重建
		"BOOT_FRESH_MS",   // 「刚启动就失败 = 页面残废」的判定已删
		"Promise.all([j('/api/query'), j('/api/accounts')])", // 改 allSettled
	} {
		if strings.Contains(src, dead) {
			t.Fatalf("页面仍残留自动重建/自动兜底逻辑：%q", dead)
		}
	}
	// 前端 fetch 不得再对网络层失败自动重试（用户要求删掉这类措施）
	if strings.Contains(src, "tries < 1 && (e instanceof TypeError") {
		t.Fatal("j() 仍在自动重试，应当直接失败交给用户点重试")
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

	// 启动判定为「今天没跑过」时必须复用同一个 checkinAll，不能另写捷径
	if !strings.Contains(src, "return checkinAll({ quiet: true });") {
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
	// 回前台的整个函数体里不能出现写请求，也不能再有重建页面的兜底
	rs := strings.Index(src, "function refreshOnResume(){")
	re := strings.Index(src[rs:], "\n  }")
	if body := src[rs : rs+re]; strings.Contains(body, "checkinAll") ||
		strings.Contains(body, "reload") || strings.Contains(body, "markDaily") {
		t.Fatalf("回前台逻辑被塞进了签到/重建动作：%s", body)
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
	// 壳层入口只有两个明确的语义：打开即判定，回前台只刷新
	for _, want := range []string{"window.__pointsAutoCheckin = bootCheck;", "window.__pointsRefresh = refreshOnResume;"} {
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
	// 0.6.4 起文案改成「一天只自动签一次」：打开 App 先查今日记录，
	// 跑过就不签。旧文案「打开 App 自动签一次」已与实际行为不符。
	for _, want := range []string{"一天只自动签一次", "切后台回来什么都不会签", "只处理「未签」的账号", "今天有没有跑过"} {
		if !strings.Contains(body, want) {
			t.Fatalf("说明缺少关键结论 %q", want)
		}
	}
}

// 设置页必须是四个分页签：通知 / 账号 / 数据 / 日志。
// 用户原话：「通知/账号/数据/日志这四个吧，外观直接删除，
//
//	主界面本来就有按钮了，设置里不再体现外观的设置。」
func TestSettingsHasFourTabs(t *testing.T) {
	src := indexHTML
	for _, want := range []string{
		"{ id: 'notify', name: '通知' }",
		"{ id: 'account', name: '账号' }",
		"{ id: 'data', name: '数据' }",
		"{ id: 'log', name: '日志' }",
		"BUILDERS = { notify: buildNotifyTab, account: buildAccountTab, data: buildDataTab, log: buildLogTab }",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("设置页缺少 %q", want)
		}
	}
	// 外观分区必须从设置页消失（主题按钮在主界面）
	start := strings.Index(src, "function openSettings(){")
	end := strings.Index(src[start:], "// ---------- 通知 Tab ----------")
	body := src[start : start+end]
	if strings.Contains(body, "'外观'") || strings.Contains(body, "THEMES.forEach") {
		t.Fatal("设置页仍在渲染外观（主题）分区，用户要求删除")
	}
}

// 「···」全删：卡片右上角和紧凑行里都不许再出现。
// 用户原话：「这个玩意不要给我保留了，占地方」「卡片不用显示···」。
func TestNoMoreDotsEntry(t *testing.T) {
	src := indexHTML
	for _, dead := range []string{
		"c-more",          // 按钮类名
		"openAccountMenu", // 旧菜单函数
		"ICON('more')",    // 三点图标
	} {
		if strings.Contains(src, dead) {
			t.Fatalf("残留「···」入口：%q", dead)
		}
	}
	// 账号详情页必须删掉（0.6.5：整行进详情「返不回去」，用户明确要求改行内按钮）
	if strings.Contains(src, "function openAccountDetail(") {
		t.Fatal("账号详情页应已删除：操作改为设置 → 账号的行内按钮")
	}
}

// 0.6.5：账号管理改成行内按钮 + 底部说明。
// 用户原话：「点进详情之后我就返不回去了，还得再从设置里面进去一遍」，
// 所以整行不再可点，令牌 / 停用 / 删除直接摆在行右侧。
func TestAccountTabInlineActions(t *testing.T) {
	src := indexHTML
	for _, want := range []string{
		"'a-btn key', '令牌'",
		"'a-btn warn', '删除'",
		"'acc-acts'",
		"'acc-note'",
		"确认删",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("账号行内操作缺少：%q", want)
		}
	}
	// 三个按钮各自的意思必须写在底部
	for _, k := range []string{"noteLine('令牌'", "noteLine('停用'", "noteLine('删除'"} {
		if !strings.Contains(src, k) {
			t.Fatalf("底部说明缺少：%q", k)
		}
	}
	// 旧提示语必须消失
	if strings.Contains(src, "点账号进入详情") {
		t.Fatal("残留旧的「点账号进入详情」提示")
	}
}

// 通知渠道是 5 个按钮，必然换行；999px 胶囊圆角换行后弧度大得离谱。
// 0.6.5 单独收成 12px / 9px 的小圆角。
func TestNotifyChannelsUseSmallRadius(t *testing.T) {
	src := indexHTML
	if !strings.Contains(src, ".seg.wrap{border-radius:12px") {
		t.Fatal("渠道选择缺少小圆角样式 .seg.wrap")
	}
	if !strings.Contains(src, "'seg wrap'") {
		t.Fatal("渠道选择没有应用 seg wrap")
	}
}

// 0.6.6：三处占空间的说明条要删掉（用户逐条点名）。
func TestRemovedVerboseNotices(t *testing.T) {
	src := indexHTML
	for _, dead := range []string{
		"备份里是明文 Cookie",
		"按执行步骤逐条记录（含成功的请求）",
		"账号管理都在这页完成",
	} {
		if strings.Contains(src, dead) {
			t.Fatalf("说明条未删除：%q", dead)
		}
	}
}

// 0.6.6：添加账号弹层底部「直接手动粘贴凭据 / 导入备份」和「关闭」按钮都要删。
// 关闭用 openSheet 的 hideCancel 选项，只影响这一个弹层。
func TestAddSheetHasNoManualAndCancel(t *testing.T) {
	src := indexHTML
	if strings.Contains(src, "直接手动粘贴凭据 / 导入备份") {
		t.Fatal("添加账号页仍有「直接手动粘贴凭据 / 导入备份」兜底按钮")
	}
	if !strings.Contains(src, "{ hideCancel: true }") {
		t.Fatal("添加账号页没有隐藏「关闭」按钮")
	}
	// 但其它弹层仍要有「关闭」（通用组件不能被误删）
	if !strings.Contains(src, "'sheet-cancel', '关闭'") {
		t.Fatal("通用弹层的「关闭」按钮被误删")
	}
}

// 0.6.6：账号名长按改备注——前端要有绑定与保存接口调用，后端要有路由。
func TestLongPressRenameLabel(t *testing.T) {
	src := indexHTML
	for _, want := range []string{
		"bindLabelLongPress",
		"openLabelEdit",
		"/api/account/label",
		"长按修改备注",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("长按改备注缺少：%q", want)
		}
	}
}

// 0.6.6：每个请求都要带 X-TZ-Offset，否则日志时间差 8 小时。
func TestRequestsCarryTZHeader(t *testing.T) {
	src := indexHTML
	if !strings.Contains(src, "opt.headers['X-TZ-Offset']") {
		t.Fatal("请求未携带 X-TZ-Offset，日志会按 UTC 记时")
	}
}

// 0.6.7：通知页的「什么时候推」四个触发开关 + 「重发上次」按钮全删。
// 用户拍板固定为「打开 App 自动 / 手动一键 / always」三态全开，
// 界面上不再有任何触发源/结果类型的分支开关。
func TestNotifyTabHasNoTriggerSwitches(t *testing.T) {
	src := indexHTML
	start := strings.Index(src, "function buildNotifyTab(")
	if start < 0 {
		t.Fatal("找不到 buildNotifyTab")
	}
	end := strings.Index(src[start:], "\n  /* ---------- ")
	if end < 0 {
		end = len(src) - start
	}
	body := src[start : start+end]
	// 「什么时候推」四个开关由 toggleRow 渲染，必须彻底消失
	for _, dead := range []string{
		"toggleRow(",           // 触发开关渲染函数
		"重发上次",                 // 重发按钮文案
		"'/api/notify/resend'", // 重发接口调用
	} {
		if strings.Contains(body, dead) {
			t.Fatalf("通知页仍残留触发开关/重发入口：%q", dead)
		}
	}
	// payload 里三态写死 true（配置数据自洽），不能读 cfg 的旧值
	if !strings.Contains(body, "on_success: true, on_failure: true, always: true,") {
		t.Fatal("通知 payload 未把结果类型写死为 true")
	}
	if !strings.Contains(body, "on_manual: true, on_start: true") {
		t.Fatal("通知 payload 未把触发源写死为 true")
	}
	// 通知页只剩：启用开关 / 渠道 / Webhook / 保存 / 发测试
	for _, want := range []string{"'启用通知'", "'渠道'", "'Webhook 地址'", "'保存'", "'发测试'"} {
		if !strings.Contains(body, want) {
			t.Fatalf("通知页缺少应有的控件：%q", want)
		}
	}
}

// 0.6.7：设置弹层顶部「设置」两字、每页版本号、底部关闭按钮全部去掉。
func TestSettingsSheetStripped(t *testing.T) {
	src := indexHTML
	start := strings.Index(src, "function openSettings(){")
	if start < 0 {
		t.Fatal("找不到 openSettings")
	}
	end := strings.Index(src[start:], "// ---------- 通知 Tab ----------")
	body := src[start : start+end]
	if !strings.Contains(body, "{ hideTitle: true, hideCancel: true }") {
		t.Fatal("设置弹层未同时隐藏标题与关闭按钮")
	}
	// 版本号 meta 不能再出现在设置页（已迁到 openHelp）
	if strings.Contains(body, "j('/api/meta')") || strings.Contains(body, "'meta', '版本") {
		t.Fatal("设置页仍残留版本号 meta 请求/渲染")
	}
	// openSheet 必须支持 hideTitle 选项（通用组件能力）
	if !strings.Contains(src, "if (!opts.hideTitle) sheet.appendChild(el('div', 'sheet-h', title))") {
		t.Fatal("openSheet 不支持 hideTitle 选项")
	}
}

// 0.6.7：版本号迁到「签到逻辑」页最下方，且该页关闭按钮去掉；
// 版本号请求同时填充 state.meta 供日志导出复用。
func TestHelpHasVersionAndNoCancel(t *testing.T) {
	src := indexHTML
	start := strings.Index(src, "function openHelp(){")
	if start < 0 {
		t.Fatal("找不到 openHelp")
	}
	end := strings.Index(src[start:], "\n  /* ---------- 设置")
	body := src[start : start+end]
	if !strings.Contains(body, "{ hideCancel: true }") {
		t.Fatal("签到逻辑页未隐藏关闭按钮")
	}
	if !strings.Contains(body, "j('/api/meta')") {
		t.Fatal("签到逻辑页未请求版本号")
	}
	if !strings.Contains(body, "state.meta = m") {
		t.Fatal("版本号结果未写入 state.meta（日志导出头会退化）")
	}
	if !strings.Contains(body, "' · 已支持 '") {
		t.Fatal("版本号行缺少「已支持 N 个平台」")
	}
}

// 0.6.8：账号页「令牌」说明里那句「只有支持令牌的平台（AiPy）才有这个按钮」删掉。
func TestTokenNoteNoPlatformHint(t *testing.T) {
	src := indexHTML
	for _, dead := range []string{
		"只有支持令牌的平台",
		"才有这个按钮",
	} {
		if strings.Contains(src, dead) {
			t.Fatalf("令牌说明仍残留平台限制提示：%q", dead)
		}
	}
	if !strings.Contains(src, "noteLine('令牌'") {
		t.Fatal("令牌说明行本身不应被删")
	}
}

// 0.6.8：数据页导出区三个按钮合成一行，顺序为 生成 → 复制 → 导出。
func TestDataTabExportButtonsInOneRow(t *testing.T) {
	src := indexHTML
	start := strings.Index(src, "function buildDataTab(")
	if start < 0 {
		t.Fatal("找不到 buildDataTab")
	}
	end := strings.Index(src[start:], "// ---------- 每日任务")
	if end < 0 {
		end = len(src) - start
	}
	body := src[start : start+end]

	// 三个按钮必须是 生成 / 复制 / 导出 的新文案
	for _, want := range []string{
		"'btn ghost', '生成'",
		"'btn ghost', '复制'",
		"'btn ghost', '导出'",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("导出按钮缺少新文案：%q", want)
		}
	}
	// 旧文案必须彻底消失
	for _, dead := range []string{"'生成备份'", "'存成文件'", "'复制到剪贴板'"} {
		if strings.Contains(body, dead) {
			t.Fatalf("导出按钮仍是旧文案：%q", dead)
		}
	}
	// append 顺序固定：生成 → 复制 → 导出；且单行 row-btns，复制不再独占一行
	seq := "expBtns.appendChild(btnExp); expBtns.appendChild(btnCopy); expBtns.appendChild(btnFile);"
	if !strings.Contains(body, seq) {
		t.Fatal("导出按钮顺序不是 生成→复制→导出")
	}
	if strings.Contains(body, "btnCopy.style.width") {
		t.Fatal("复制按钮仍被设成独占一行")
	}
}

// 0.6.9 方案 A2：前端 j() 必须给 POST body 加 X-Points-Body 兜底头。
// iOS 走 pointsapi:// 自定义 scheme 时，个别 iOS 版本会 strip 掉
// WKURLSchemeHandler 请求的 httpBody；原生层回退读这个 header。
// 桌面 HTTP server 不认这个头、自动忽略，所以全局加上无副作用。
func TestPointsBodyFallbackHeader(t *testing.T) {
	src := indexHTML
	for _, want := range []string{
		"opt.headers['X-TZ-Offset'] = String(new Date().getTimezoneOffset());",
		"if (opt.body) opt.headers['X-Points-Body'] = encodeURIComponent(opt.body);",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("j() 缺少 A2 兜底：%q", want)
		}
	}
}
