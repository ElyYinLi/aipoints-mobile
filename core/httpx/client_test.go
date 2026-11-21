package httpx

import (
	"net/http"
	"strings"
	"testing"
)

// 「回前台第一次读取必失败」的根因是复用了系统已回收的连接。
// 这条测试钉住修法：客户端必须不走 keep-alive。
// 改回默认 Transport 就等于把这个问题重新引回来，而且是偶发、极难复现的那种。
func TestClientDisablesKeepAlive(t *testing.T) {
	c := New()
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport 类型不对：%T", c.HTTP.Transport)
	}
	if !tr.DisableKeepAlives {
		t.Fatal("必须禁用 keep-alive：否则 App 切后台再回来会复用死连接，首个请求必失败")
	}
	if c.HTTP.Timeout <= 0 {
		t.Fatal("必须保留总超时，否则上游挂住会一直转")
	}
}

// 日志时间必须精确到毫秒：一轮签到里「查询 / 签到 / 核对」常在同一秒内完成，
// 只到秒就看不出先后顺序（用户要的是「能看出执行逻辑」）。
func TestLogTimeHasMillis(t *testing.T) {
	ClearLogs()
	AddLog(LogEntry{Msg: "毫秒时间戳测试"})
	logs := Logs(0)
	if len(logs) != 1 {
		t.Fatalf("日志条数 %d", len(logs))
	}
	// HH:MM:SS.mmm = 12 字符，且有小数点
	if len(logs[0].Time) != 12 || !strings.Contains(logs[0].Time, ".") {
		t.Fatalf("时间格式不是 HH:MM:SS.mmm：%q", logs[0].Time)
	}
}

// 脱敏只盖住凭据本身，不能把同一行后半句的错误原因一起吞掉。
// 0.6.3 的写法是 `[^",;\r\n}]*`（不含空白），导致
// 「cookie=BDUSS=xxx 出错了」整句变成「cookie=***」，真正的线索没了。
func TestMaskTextStopsAtWhitespace(t *testing.T) {
	got := maskText("cookie=BDUSS=abcdef123456 出错了：连接超时")
	if strings.Contains(got, "abcdef123456") {
		t.Fatalf("凭据没盖住：%q", got)
	}
	if !strings.Contains(got, "出错了") {
		t.Fatalf("后半句错误原因被一起吞掉了：%q", got)
	}
}

// 轮次 id 要能自动继承到同一轮的所有日志上——这是把一次执行串成时间线的前提。
func TestLogRoundInherited(t *testing.T) {
	ClearLogs()
	restore := SetRound("a1b2")
	AddLog(LogEntry{Msg: "轮内第一条"})
	restore()
	AddLog(LogEntry{Msg: "轮外"})

	logs := Logs(0)
	if len(logs) != 2 {
		t.Fatalf("日志条数 %d", len(logs))
	}
	if logs[0].Round != "a1b2" {
		t.Fatalf("轮内日志没带 round：%+v", logs[0])
	}
	if logs[1].Round != "" {
		t.Fatalf("轮外日志不该带 round：%+v", logs[1])
	}
}
