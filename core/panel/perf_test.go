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

// A1：回前台调 JS 必须延后。
//
// 原因见 PointsApp.swift 内注释：iOS 挂起 WebContent 后 setAllowsActivities(false)，
// 此时 runJavaScriptInFrameInScriptWorld（evaluateJavaScript 的底层）会被拒。
// 解除挂起依赖 RunningBoard 的异步回调，一 didBecomeActive 就发必然撞上。
func TestResumeRefreshIsDeferred(t *testing.T) {
	src := swiftSource(t)
	if !strings.Contains(src, "asyncAfter(deadline: .now() + 0.35)") {
		t.Fatal("回前台调 JS 未延后：必须 asyncAfter 0.35s 避开 WebKit 拒绝窗口")
	}
	// 不能再出现「didBecomeActive 里直接 evaluateJavaScript」的写法
	idx := strings.Index(src, "didBecomeActiveNotification")
	if idx < 0 {
		t.Fatal("找不到 didBecomeActiveNotification 处理块")
	}
	tail := src[idx:]
	if end := strings.Index(tail, ".fullScreenCover"); end > 0 {
		tail = tail[:end]
	}
	if strings.Contains(tail, "evaluateJavaScript") && !strings.Contains(tail, "asyncAfter") {
		t.Fatal("didBecomeActive 处理块里仍直接 evaluateJavaScript")
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
