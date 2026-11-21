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
