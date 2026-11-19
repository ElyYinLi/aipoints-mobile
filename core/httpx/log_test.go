package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLogMasksCredential 日志绝不能漏凭据：用户会把它复制到聊天窗口求助。
func TestLogMasksCredential(t *testing.T) {
	ClearLogs()
	AddLog(LogEntry{
		Level: "error", Scope: "http",
		Msg: `failed cookie=BDUSS=abc123def456; bce-user-info="xxyyzz" authorization: Bearer sk-abcdefgh12345`,
		// 32 位以上连续 hex 也要盖掉（常见 token 形态）
		Response: `{"token":"0123456789abcdef0123456789abcdef"}`,
	})
	logs := Logs(0)
	if len(logs) != 1 {
		t.Fatalf("期望 1 条，实际 %d", len(logs))
	}
	got := logs[0].Msg + " " + logs[0].Response
	for _, leak := range []string{"abc123def456", "xxyyzz", "sk-abcdefgh12345", "0123456789abcdef0123456789abcdef"} {
		if strings.Contains(got, leak) {
			t.Fatalf("日志泄漏了凭据 %q: %s", leak, got)
		}
	}
}

// TestLogRingCap 超出容量丢最旧的，且不 panic。
func TestLogRingCap(t *testing.T) {
	ClearLogs()
	for i := 0; i < logCap+50; i++ {
		AddLog(LogEntry{Scope: "t", Msg: "x"})
	}
	if n := len(Logs(0)); n != logCap {
		t.Fatalf("容量上限应为 %d，实际 %d", logCap, n)
	}
}

// TestLogPath 去掉 query 与协议，只留 host+path。
//
// 目的：query 里常带 token / ticket，落日志等于泄漏。
func TestLogPath(t *testing.T) {
	cases := map[string]string{
		"https://dumate.baidu.com/api/x?a=1&ticket=secret": "dumate.baidu.com/api/x",
		"http://127.0.0.1:7879/api/logs?limit=200":         "127.0.0.1:7879/api/logs",
		"https://u:p@h.com/a":                              "h.com/a",
	}
	for in, want := range cases {
		if got := LogPath(in); got != want {
			t.Fatalf("%s → %q，期望 %q", in, got, want)
		}
	}
}

// TestDoJSONLogsFailure 失败请求要自动留痕，成功请求不刷屏。
func TestDoJSONLogsFailure(t *testing.T) {
	ClearLogs()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/bad") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":410110,"msg":"抽奖请求ID无效"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	c := New()
	if _, err := c.DoJSON(context.Background(), "GET", srv.URL+"/good", nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(Logs(0)); n != 0 {
		t.Fatalf("成功请求不该写日志，实际 %d 条", n)
	}

	if _, err := c.DoJSON(context.Background(), "POST", srv.URL+"/bad", nil, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	logs := Logs(0)
	if len(logs) != 1 {
		t.Fatalf("失败请求应写 1 条，实际 %d", len(logs))
	}
	e := logs[0]
	if e.Code != 410110 {
		t.Fatalf("应记录业务码 410110，实际 %v", e.Code)
	}
	if !strings.Contains(e.Msg, "请求ID无效") {
		t.Fatalf("应记录服务端消息，实际 %q", e.Msg)
	}
	if e.Level != "warn" {
		t.Fatalf("HTTP 200 + 业务码异常应为 warn，实际 %q", e.Level)
	}
}
