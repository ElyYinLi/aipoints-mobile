package panel

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 0.7.1：WKURLSchemeTask 的失效保护。
//
// WKURLSchemeTask 有条硬规则：一旦 WebKit 调用 `webView(_:stop:)`，
// 之后再对它调 didReceive / didFinish 会抛 NSInternalInconsistencyException，
// **直接崩溃**。
//
// 这在本版是必然会踩到的：回前台检测到进程冻结就重建页面，重建必然 cancel
// 掉上一轮所有 in-flight 请求；而 Go 侧的 MobilePanelDispatch 无法取消，
// 它跑完后照样会回主线程回调。没有失效检查 = 稳定可复现的崩溃。
//
// 旧代码在注释里写了「task 已停止——忽略，避免崩溃」，但代码里根本没检查。
func TestSchemeTaskInvalidationGuard(t *testing.T) {
	src := swiftSourceNamed(t, "PanelSchemeHandler.swift")

	// 必须有活跃 task 集合
	if !strings.Contains(src, "liveTasks") {
		t.Fatal("缺少 liveTasks：task 失效后仍会回调 didReceive，导致崩溃")
	}
	// start 时登记
	idx := strings.Index(src, "func webView(_ webView: WKWebView, start urlSchemeTask: WKURLSchemeTask)")
	if idx < 0 {
		t.Fatal("找不到 start 方法")
	}
	seg := src[idx:]
	if end := strings.Index(seg, "func webView(_ webView: WKWebView, stop"); end > 0 {
		seg = seg[:end]
	}
	if !strings.Contains(seg, "liveTasks.insert") {
		t.Fatal("start 时没有登记 task")
	}
	// 回调前必须已检查
	if !strings.Contains(seg, "guard self.liveTasks.remove(taskKey) != nil else { return }") {
		t.Fatal("回调前没有检查 task 是否已失效")
	}
	// stop 时必须移除（这是唯一的失效信号）
	stopIdx := strings.Index(src, "func webView(_ webView: WKWebView, stop urlSchemeTask: WKURLSchemeTask)")
	if stopIdx < 0 {
		t.Fatal("找不到 stop 方法")
	}
	stopSeg := src[stopIdx:]
	if !strings.Contains(stopSeg, "liveTasks.remove") {
		t.Fatal("stop 里没有移除 task，失效保护形同虚设")
	}
}

func swiftSourceNamed(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	p := filepath.Join(repoRoot, "ios", "App", "Sources", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", name, err)
	}
	return string(b)
}

// 0.7.2：并发闸门 + 请求超时兜底。
//
// 为什么需要闸门：Go 的导出函数是同步阻塞的，一个请求占一条 GCD 线程。
// App 在后台待久了，页面可能积压若干 in-flight 请求；回前台一股脑发出去，
// global 队列（上限约 64 条线程）会被占满，后续所有接口一起排队——
// 用户看到的就是「点哪都没反应」。限死 8 条，多的老实排队。
//
// 为什么需要超时：Go 侧网络请求都有 context 超时，正常情况下一定返回。
// 但万一卡在某个没设超时的地方，页面就会永远停在 loading，同样表现为
// 「点了没反应」。这里兜一层，让页面拿到 504 去走它自己的重试 UI。
func TestSchemeHasGateAndTimeout(t *testing.T) {
	src := swiftSourceNamed(t, "PanelSchemeHandler.swift")

	if !strings.Contains(src, "DispatchSemaphore(value: 8)") {
		t.Fatal("缺少并发闸门：无限制并发会把 GCD 线程占满，所有接口一起排队")
	}
	if !strings.Contains(src, "gate.wait()") || !strings.Contains(src, "gate.signal()") {
		t.Fatal("信号量没有成对使用，会永久泄漏槽位")
	}
	// 超时必须按接口区分：签到本身就要跑很久，20s 会把它误杀
	if !strings.Contains(src, "isLong") {
		t.Fatal("缺少长任务判断，签到会被超时兜底误杀")
	}
	if !strings.Contains(src, "180.0") || !strings.Contains(src, "20.0") {
		t.Fatal("超时阈值不是 180s（长任务）/ 20s（普通接口）")
	}
	// 只结算一次：正常返回与超时兜底互斥
	if !strings.Contains(src, "OnceFlag") || !strings.Contains(src, "once.fire()") {
		t.Fatal("缺少一次性闸门：正常返回和超时兜底会重复 didFinish")
	}
	// 504 要带可读文案，页面能直接显示
	if !strings.Contains(src, "504") || !strings.Contains(src, "请求超时") {
		t.Fatal("超时兜底没有返回 504 + 可读文案")
	}
}
