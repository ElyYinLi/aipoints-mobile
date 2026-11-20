package provider

import "testing"

// Classify 的顺序敏感：auth 必须先于 blocked，already 必须先于 blocked。
// 这组用例就是守着这个顺序，改顺序必挂。
func TestClassifyOrder(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   ErrKind
	}{
		// 「无权进行此操作」含「操作」，不能被 blocked 的「不可领取」抢走
		{403, `{"success":false,"message":"无权进行此操作"}`, ErrAuth},
		// 「今日已签到」含「签到时间」的子串风险，already 必须先命中
		{200, `{"success":false,"message":"今日已签到"}`, ErrAlready},
		{200, `{"success":false,"message":"已领取过该奖励"}`, ErrAlready},
		{200, `{"success":false,"message":"今日命中安全验证，请完成人机验证"}`, ErrCaptcha},
		{200, `{"success":false,"message":"请求过于频繁，请稍后再试"}`, ErrRateLimit},
		{200, `{"success":false,"message":"不在签到时间窗口内"}`, ErrBlocked},
		{200, `{"success":false,"message":"账号已被管理员限制"}`, ErrBlocked},
		{200, `{"success":false,"message":"奖励已冻结"}`, ErrBlocked},
		// 正文没有关键词时退回状态码
		{500, `oops`, ErrServer},
		{502, ``, ErrServer},
		{401, ``, ErrAuth},
		{429, ``, ErrRateLimit},
		{418, ``, ErrUnknown},
		{200, `{"success":true}`, ErrNone},
	}
	for _, c := range cases {
		if got := Classify(c.status, c.body); got != c.want {
			t.Errorf("Classify(%d, %q) = %q, 期望 %q", c.status, c.body, got, c.want)
		}
	}
}

// 只有三类值得重试：网络、服务端、限流。
// 其它一律不重试——重试已签到/验证码只会让请求数翻倍，正是用户抱怨的「容易黑号」。
func TestIsRetryable(t *testing.T) {
	yes := []ErrKind{ErrNetwork, ErrServer, ErrRateLimit}
	no := []ErrKind{ErrAuth, ErrAlready, ErrCaptcha, ErrBlocked, ErrUnknown, ErrNone}
	for _, k := range yes {
		if !(&APIError{Kind: k}).IsRetryable() {
			t.Errorf("%q 应该可重试", k)
		}
	}
	for _, k := range no {
		if (&APIError{Kind: k}).IsRetryable() {
			t.Errorf("%q 不应可重试", k)
		}
	}
}

func TestAsAPIError(t *testing.T) {
	if AsAPIError(nil) != nil {
		t.Fatal("nil 应返回 nil")
	}
	orig := &APIError{Kind: ErrBlocked, Message: "x"}
	if got := AsAPIError(orig); got != orig {
		t.Fatal("*APIError 应原样返回")
	}
	got := AsAPIError(errString("boom"))
	if got.Kind != ErrUnknown || got.Message != "boom" {
		t.Fatalf("普通 error 应归为 unknown 并保留原文: %+v", got)
	}
}

func TestClassifyResponseTruncatesBody(t *testing.T) {
	long := make([]byte, 2000)
	for i := range long {
		long[i] = 'a'
	}
	ae := ClassifyResponse(500, long, "", "/api/x")
	if ae.Kind != ErrServer || ae.Status != 500 || ae.Path != "/api/x" {
		t.Fatalf("分类字段不对: %+v", ae)
	}
	if len(ae.Body) > 900 {
		t.Fatalf("Body 应截断到 800 字符，实际 %d", len(ae.Body))
	}
	// Message 为空时补状态码，避免界面显示空白
	if ae.Message == "" {
		t.Fatal("Message 不应为空")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
