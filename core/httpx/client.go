// Package httpx 是极简 HTTP 工具层：只用标准库，目标是能在 gomobile 里安全使用，
// 且不引入任何第三方依赖（移动端包体与构建时间都要省）。
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client 带默认超时的 HTTP 客户端。移动网络下 20s 足够，超时即失败由上层重试。
type Client struct {
	HTTP *http.Client
}

// New 构造默认客户端。
//
// 关键：DisableKeepAlives = true（0.6.4 修的根因）。
//
// 之前「切回前台第一次读取必失败、点重新加载也没用」的真正原因是连接复用：
// App 进后台后，内核会悄悄回收空闲 TCP 连接，但 Go 的连接池还以为它是活的，
// 回到前台第一个请求就把数据写进一个已经死掉的 socket，等不到响应直接报错。
// 而 WebView 里那一轮 fetch 全都命中这个坏连接，看起来就是「整个页面死了」。
//
// 代价是每次请求多一次 TCP+TLS 握手（回环/外网各几十毫秒），
// 换来的确定性远大于这点开销——我们一轮最多十几个请求，不是长连接场景。
// 客户端有 20s 超时兜底，不需要靠 keep-alive 撑性能。
func New() *Client {
	return &Client{HTTP: &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives:      true,
			MaxIdleConns:           0,
			IdleConnTimeout:        time.Second,
			TLSHandshakeTimeout:    10 * time.Second,
			ExpectContinueTimeout:  1 * time.Second,
			MaxResponseHeaderBytes: 64 << 10,
		},
	}}
}

// Response 是脱敏后的响应：只保留状态码与正文，绝不打印请求头（避免 cookie 泄漏）。
type Response struct {
	Status int
	Body   []byte
}

// DoJSON 发一次 JSON 请求。headers 为 nil 时只带最小头。
//
// 注意：body 传 nil 表示无请求体（GET）；传空 map 会发送 `{}`（部分接口要求显式空对象）。
func (c *Client) DoJSON(ctx context.Context, method, url string, headers map[string]string, body any) (*Response, error) {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	start := time.Now()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// 用户连续下拉刷新时，上一轮查询会被 context 取消，这在 UI 上是正常行为。
		// 把它记成 error 会瞬间刷满 400 条把真问题盖掉，所以直接丢弃。
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		// 网络层失败要留痕：用户报「签到不动」时，这行日志能直接区分
		// 「请求没发出去」和「服务端拒绝」。
		// 轮次号会自动继承（AddLog 里读 SetRound），这里不用显式传；
		// step 由调用方在上层记，这一行只负责「哪个地址、什么原因」。
		AddLog(LogEntry{
			Level: "error", Scope: "http", Method: method, Path: LogPath(url),
			CostMS: time.Since(start).Milliseconds(), Msg: "请求失败: " + err.Error(),
		})
		return nil, err
	}
	defer resp.Body.Close()
	// 限制读取上限，防止异常响应撑爆内存
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	out := &Response{Status: resp.StatusCode, Body: data}
	logResponse(method, url, out, time.Since(start))
	return out, nil
}

// LogOK 记录一条「请求成功」的日志。
//
// 0.6.4：原本只在出错时记，用户的原话是「正常日志我一点都看不到，
// 非得出了错才有日志」。现在成功请求也记，但由调用方在**业务步骤边界**记，
// 而不是在 logResponse 里对每个请求无脑记一遍：
//   - 调用方知道自己在干什么（「签到 百度搭子」「领任务奖励」），
//     记出来的是执行逻辑，不是一堆看不出用途的 URL；
//   - 一次签到会有上游的拉状态、提交、回查三次请求，全记会稀释关键行。
func LogOK(scope, step, method, url string, status int, cost time.Duration) {
	AddLog(LogEntry{
		Level: "info", Scope: scope, Step: step, Method: method, Path: LogPath(url),
		Status: status, CostMS: cost.Milliseconds(), Msg: "成功",
	})
}

// logResponse 给一次请求留一条日志。
//
// 只在「非 2xx」或「body 里 code != 0」时记 warn/error —— 正常查询每次都记会
// 把日志刷满，用户翻了半天看不到真问题。成功请求仅在 debug 意义下有意义，
// 这里刻意不记，保持日志信噪比。
func logResponse(method, url string, r *Response, cost time.Duration) {
	bad := r.Status < 200 || r.Status >= 300
	var bizCode float64
	var hasCode bool
	var msg string
	if m, err := r.JSON(); err == nil {
		if v, ok := Num(m["code"]); ok {
			bizCode, hasCode = v, true
			// 百度系 0 成功、HarmonyOS 系 200 成功，其余都算异常
			bad = bad || (v != 0 && v != 200)
		}
		if bad {
			msg = Str(m["msg"])
			if msg == "" {
				msg = Str(m["message"])
			}
		}
	}
	// 一切正常就不记：日志只留异常，信噪比才够高
	if !bad {
		return
	}
	lvl := "warn"
	if r.Status >= 400 || r.Status == 0 {
		lvl = "error"
	}
	snippet := ""
	if msg == "" {
		snippet = string(r.Body)
	}
	e := LogEntry{
		Level: lvl, Scope: "http", Method: method, Path: LogPath(url),
		Status: r.Status, CostMS: cost.Milliseconds(), Msg: msg, Response: snippet,
	}
	if hasCode {
		e.Code = bizCode
	}
	AddLog(e)
}

// JSON 把正文解析成 map。解析失败时给出截断后的正文片段便于排查（不含敏感头）。
func (r *Response) JSON() (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		// 网关层错误（401/403/502）常返回 HTML，此时优先给出状态码语义
		if r.Status == 401 || r.Status == 403 {
			return nil, fmt.Errorf("鉴权失败(%d)：凭据无效或已过期", r.Status)
		}
		if r.Status >= 500 {
			return nil, fmt.Errorf("服务端异常(%d)，请稍后重试", r.Status)
		}
		snippet := string(r.Body)
		if len(snippet) > 120 {
			snippet = snippet[:120]
		}
		return nil, fmt.Errorf("http %d: 非 JSON 响应: %s", r.Status, snippet)
	}
	return m, nil
}

// ErrNoCredential 是缺凭据时的统一文案，壳层可据此引导用户去登录。
var ErrNoCredential = errors.New("缺少凭据，请先登录")

// Pick 按路径逐层取值，任一层缺失返回 nil。
// 例如 Pick(m, "data", "Packages") 。
func Pick(m map[string]any, path ...string) any {
	var cur any = m
	for _, key := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[key]
	}
	return cur
}

// PickSlice 取数组，缺失或类型不符返回 nil。
func PickSlice(m map[string]any, path ...string) []any {
	v := Pick(m, path...)
	s, _ := v.([]any)
	return s
}

// Num 把任意 JSON 数字/字符串数字转成 float64。
func Num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(x, "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

// Str 把任意值转成字符串（数字保留原始精度由调用方处理）。
func Str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%v", x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return ""
}

// Bool 宽松布尔：兼容 true/1/"true"/"1"。
func Bool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x == "true" || x == "1"
	}
	return false
}
