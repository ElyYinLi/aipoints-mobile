package provider

import (
	"strconv"
	"strings"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// 这里的辅助函数是给「响应结构不确定」的场景用的。
// 逆向只能拿到接口形状，拿不到真实响应体，所以解析一律走多候选 + 宽松类型，
// 宁可降级显示也不 panic。

// pick 从多个候选键里取第一个存在的值。
func pick(m map[string]any, keys ...string) any {
	if m == nil {
		return nil
	}
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

// digMap 逐层下钻 map，任一层不存在返回 nil。
func digMap(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

// numOf 宽松取数字：数字、字符串数字、nil 都能处理。
func numOf(v any) float64 {
	n, _ := httpx.Num(v)
	return n
}

// boolOf 宽松取布尔：true/"true"/1/"1" 都算真。
func boolOf(v any) bool { return httpx.Bool(v) }

// strOf 取字符串。
func strOf(v any) string { return httpx.Str(v) }

// fmtNum 去掉无意义的小数位：1500.0 -> "1500"，1500.5 -> "1500.5"。
func fmtNum(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

// trimSlash 去掉尾部斜杠与空白。
func trimSlash(s string) string {
	return strings.TrimSuffix(strings.TrimSpace(s), "/")
}

// hasScheme 判断是否已带协议头（便于测试注入 httptest 的 http://）。
func hasScheme(s string) bool { return strings.Contains(s, "://") }

// digAny 在可能被 data/result/resp/response 包裹的响应里逐层找第一个命中的字段。
//
// 为什么需要：各家网关的信封不统一，同一个字段有时在顶层、有时在 data 下。
// 逐个写死路径会在上游改一次包装层后全面失效（表现是「余额一直显示 —」）。
// 参考实现：Minatoxiaohu/agent-auto-signin 的 dig()。
func digAny(m map[string]any, keys ...string) any {
	return digAnyDepth(m, 0, keys...)
}

func digAnyDepth(m map[string]any, depth int, keys ...string) any {
	if m == nil || depth > 4 {
		return nil
	}
	if v := pick(m, keys...); v != nil {
		return v
	}
	// 先按已知信封键下钻，再对任意子对象兜底：上游偶尔会多套一层业务对象
	// （如 {"data":{"growth":{"today_checked_in":true}}}），只认固定键会漏。
	for _, k := range []string{"data", "result", "resp", "response"} {
		if next, ok := m[k].(map[string]any); ok {
			if v := digAnyDepth(next, depth+1, keys...); v != nil {
				return v
			}
		}
	}
	for _, val := range m {
		if next, ok := val.(map[string]any); ok {
			if v := digAnyDepth(next, depth+1, keys...); v != nil {
				return v
			}
		}
	}
	return nil
}
