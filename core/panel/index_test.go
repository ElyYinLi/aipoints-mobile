package panel

import (
	"strings"
	"testing"
)

// 用户反复投诉过三次的规则：「今日已签」后面必须永远带「+N 单位」。
// 这条规则只存在于模板 JS 里，Go 编译不检查，所以用字符串断言把它钉住——
// 一旦有人再把 `if (today_gain > 0)` 的省略逻辑写回去，测试立刻红。
func TestIndexDetailTextAlwaysShowsGain(t *testing.T) {
	src := indexHTML

	cases := []struct{ name, want string }{
		{"已签必带 +N", "return '今日已签 +' + (g || '?') + ' ' + unit;"},
		{"收益数值统一取值", "function gainNum(r){"},
		{"未签可得也带单位", "return '今日未签 · 可得 +' + g2 + ' ' + unit;"},
	}
	for _, c := range cases {
		if !strings.Contains(src, c.want) {
			t.Fatalf("%s：模板里缺少 %q", c.name, c.want)
		}
	}
	// 反面断言：绝不能出现「只写今日已签、不带数字」的裸分支
	if strings.Contains(src, "return '今日已签';\n") {
		t.Fatal("detailText 里仍有裸的「今日已签」返回——会吞掉 +N")
	}
}

// 「跟随系统」不能只靠 media query：Android WebView 上它常年失灵。
// 必须有壳层注入的真值通道 + 轮询兜底，否则用户切系统深色后页面不跟。
func TestIndexFollowsSystemTheme(t *testing.T) {
	src := indexHTML
	for _, want := range []string{
		"window.__pointsOnSystemTheme",
		"window.__pointsSystemDark",
		"setInterval(function(){",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("主题跟随系统缺少 %q", want)
		}
	}
}
