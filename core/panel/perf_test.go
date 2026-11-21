package panel

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 卡顿修复的源码级约束。
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

// 回前台久留之后必须换**实例**，不能只 load 也不能只探活。
//
// 演进（三次都失败过，别再退回去）：
//
//	0.7.0  350ms 延后调 JS         → 实测无效
//	0.7.1  探活(1+1) + 两级重建    → 实测仍无效（用户：卡顿恢复不了，一直卡）
//	0.7.2  离开 >= 10s 直接换实例  → 不再判断进程死活，因为卡死时系统不回话
//
// 为什么不能靠探活：探针本身要走一次 JS 往返，而「卡住」意味着这次往返
// 可能既不回调也不报错；700ms 的兜底又靠 main.asyncAfter，主线程一忙同样
// 排不进去。判断链越长，失败点越多。
func TestResumeHardRebuildsWebView(t *testing.T) {
	src := swiftSource(t)

	if !strings.Contains(src, "func hardReset()") {
		t.Fatal("缺少 hardReset：回前台久留必须换新 WKWebView")
	}
	// 必须真的换实例（SwiftUI 靠 .id 重建 UIViewRepresentable）
	if !strings.Contains(src, "webGen = UUID()") || !strings.Contains(src, ".id(webGen)") {
		t.Fatal("缺少 webGen / .id(webGen)：没有这一步就只是旧实例上重新 load，救不回来")
	}
	// 旧实例必须先断开回调，否则它的异步回调会污染新状态
	idx := strings.Index(src, "func hardReset()")
	tail := src[idx:]
	if end := strings.Index(tail, "\n    /// "); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "navigationDelegate = nil") {
		t.Fatal("hardReset 未断开旧实例的 navigationDelegate")
	}
	// 0.7.1 的探活/两级重建必须已经删掉，不能两套并存
	if strings.Contains(src, "recoverToken") {
		t.Fatal("仍有 recoverToken：0.7.1 的两级结构已废，只保留 webGen 一条路")
	}
	if strings.Contains(src, "func reviveOrRebuild") {
		t.Fatal("仍有 reviveOrRebuild：探活不该再作为主路径")
	}
}

// 三档时间阈值必须都在。
func TestResumeHasThreeTiers(t *testing.T) {
	src := swiftSource(t)

	// < 1.5s：快速切走又回来，只刷新
	if !strings.Contains(src, "away < 1.5") {
		t.Fatal("缺少「离开 < 1.5s 直接刷新」的快路径")
	}
	// < 10s：签到可能正在跑，先探活，别打断
	if !strings.Contains(src, "away < 10") {
		t.Fatal("缺少「1.5~10s 探活」的中档，会把正在跑的签到打断")
	}
	// 探针仍有 800ms 兜底
	if !strings.Contains(src, ".now() + 0.8") {
		t.Fatal("缺少 0.8s 探活超时：冻结时回调不会来")
	}
	// >= 10s：换实例
	if !strings.Contains(src, "hardReset()") {
		t.Fatal("缺少 hardReset 调用")
	}
}

// 重建失败要有原生出口，不能把用户丢在白屏。
func TestRebuildFailureHasNativeRetry(t *testing.T) {
	src := swiftSource(t)
	if !strings.Contains(src, "stalled") {
		t.Fatal("缺少 stalled 状态：3s 没加载完必须给用户一个明确出口")
	}
	if !strings.Contains(src, "页面没有响应") {
		t.Fatal("缺少重建失败提示")
	}
	if !strings.Contains(src, `.now() + 3.0`) {
		t.Fatal("缺少 3s 重建超时判定")
	}
}

// 性能相关的 WebView 配置不能被改回去。
func TestWebViewPerfFlags(t *testing.T) {
	src := swiftSource(t)

	// isInspectable 只在 Debug 开：release 下它会让 WebKit 常驻保留调试元信息
	if !strings.Contains(src, "#if DEBUG") {
		t.Fatal("缺少 #if DEBUG：isInspectable 不能在 release 开")
	}
	// 不透明合成路径
	if !strings.Contains(src, "web.isOpaque = true") {
		t.Fatal("WebView 必须不透明：透明会让每帧都和父视图重新合成")
	}
	// 主题重复赋值不能继续发 objectWillChange
	if strings.Contains(src, "@Published var dark") {
		t.Fatal("dark 仍是 @Published：相同值也会触发根视图重算")
	}
	if !strings.Contains(src, "guard newValue != dark else { return }") {
		t.Fatal("dark 缺少同值短路")
	}
}

// WebContent 进程被 kill 的兜底 + 冷却，一条都不能少。
func TestWebContentProcessTerminateHandled(t *testing.T) {
	src := swiftSource(t)
	if !strings.Contains(src, "func webViewWebContentProcessDidTerminate") {
		t.Fatal("缺少 webViewWebContentProcessDidTerminate，进程被 kill 后页面不会恢复")
	}
	idx := strings.Index(src, "func webViewWebContentProcessDidTerminate")
	tail := src[idx:]
	if end := strings.Index(tail, "func webView("); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "PanelSchemeHandler.baseURL") {
		t.Fatal("兜底回调没有重新加载面板地址")
	}
	if !strings.Contains(src, "lastRecoverAt") || !strings.Contains(src, "timeIntervalSince(last) < 60") {
		t.Fatal("缺少 60s 冷却：崩溃循环会反复闪白")
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
