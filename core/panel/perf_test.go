package panel

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 0.7.0 卡顿修复的源码级约束。
//
// 这些问题没法在单元测试里复现（要真机 + 长时间后台），
// 但它们都是「删一行就复发」的回归点，必须用测试钉住。
func swiftSource(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	p := filepath.Join(repoRoot, "ios", "App", "Sources", "PointsApp.swift")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 PointsApp.swift 失败：%v", err)
	}
	return string(b)
}

// A1：回前台必须先探活，死了要重建。
//
// 演进：0.7.0 只是「延后 350ms 调 JS」，想避开 setAllowsActivities(false) 的拒绝窗口。
// 实测无效——用户锁屏 4 分钟回来「点哪都没反应」。真因是 WebContent 进程被冻结后
// 根本没恢复（WebKit Bug 321511），页面收不到任何事件，客户端无法唤醒，只能重建。
func TestResumeProbeAndRebuild(t *testing.T) {
	src := swiftSource(t)

	// 必须有探活函数
	if !strings.Contains(src, "func reviveOrRebuild") {
		t.Fatal("缺少 reviveOrRebuild：回前台必须探活，不能盲发刷新")
	}
	// 探针用 1+1：返回值确定，能区分「真的执行了」和「返回 undefined」
	if !strings.Contains(src, `evaluateJavaScript("1+1")`) {
		t.Fatal("探针不是 1+1，无法判断进程是否真的响应")
	}
	// 必须有超时兜底（进程冻结时 completionHandler 永远不来）
	if !strings.Contains(src, ".now() + 0.7") {
		t.Fatal("缺少 0.7s 探活超时：冻结时回调不会来")
	}
	// 必须有重建
	if !strings.Contains(src, "func rebuild(") {
		t.Fatal("缺少 rebuild：进程冻结只能重建，无法唤醒")
	}
	// 二级恢复：连 load 都发不出去时销毁重建 WKWebView
	if !strings.Contains(src, "recoverToken") || !strings.Contains(src, ".id(recoverToken)") {
		t.Fatal("缺少 recoverToken 二级恢复：进程池卡死时只有新建 WKWebView 能救")
	}
	// 快速切换不要误伤
	if !strings.Contains(src, "away < 1.5") {
		t.Fatal("缺少「离开 < 1.5s 直接刷新」的快路径")
	}
}

// A2：必须实现 WebContent 进程终止兜底。
//
// 没这个回调时，进程被系统 kill 掉后页面就是死的：点什么都不响应，
// 也不会自动重载。这是「整体卡顿」里最严重的一种。
func TestWebContentProcessTerminateHandled(t *testing.T) {
	src := swiftSource(t)
	if !strings.Contains(src, "func webViewWebContentProcessDidTerminate") {
		t.Fatal("缺少 webViewWebContentProcessDidTerminate，进程被 kill 后页面不会恢复")
	}
	// 必须真的重新加载面板，不能只打日志
	idx := strings.Index(src, "func webViewWebContentProcessDidTerminate")
	tail := src[idx:]
	if end := strings.Index(tail, "func webView("); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "PanelSchemeHandler.baseURL") {
		t.Fatal("兜底回调没有重新加载面板地址")
	}
}

// A3：兜底必须带冷却，防止进程反复崩溃时无限 reload。
func TestTerminateRecoveryHasCooldown(t *testing.T) {
	src := swiftSource(t)
	if !strings.Contains(src, "lastRecoverAt") {
		t.Fatal("缺少 lastRecoverAt，reload 无冷却会在崩溃循环里反复闪白")
	}
	if !strings.Contains(src, "timeIntervalSince(last) < 60") {
		t.Fatal("冷却窗口不是 60s")
	}
}

// B2：毛玻璃半径必须压在 12px 以内，且必须有 @supports 降级。
//
// backdrop-filter 每帧都要对背后内容重新采样，半径越大开销越高；
// 而 .appbar 是 sticky、.bar 是 fixed，两者长期停在屏幕上。
func TestBackdropBlurIsBounded(t *testing.T) {
	src := panelSource(t)
	if strings.Contains(src, "blur(18px)") {
		t.Fatal("仍有 blur(18px)，半径过大（建议 8~12px）")
	}
	if strings.Contains(src, "blur(24px)") || strings.Contains(src, "blur(20px)") {
		t.Fatal("毛玻璃半径超过 12px 上限")
	}
	// 两处（appbar / bar）都必须有 @supports 降级
	if got := strings.Count(src, "@supports ((backdrop-filter:blur(1px))"); got < 2 {
		t.Fatalf("@supports 降级块应有 2 个（appbar + bar），实际 %d", got)
	}
}

// B3：卡片不能常驻 will-change，否则每张卡都占一个合成层。
func TestCardHasNoWillChange(t *testing.T) {
	src := panelSource(t)
	// 允许出现在注释里，但不能是生效的 CSS 声明
	for _, line := range strings.Split(src, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "will-change:") {
			t.Fatalf("仍有生效的 will-change 声明：%s", l)
		}
	}
}

func panelSource(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(thisFile), "index.go")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 index.go 失败：%v", err)
	}
	return string(b)
}

var _ = path.Join
