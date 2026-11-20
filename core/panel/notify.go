package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// notifier 负责「签到结果推送到企业微信 / 钉钉 / 飞书 / Bark / 自定义」。
//
// 为什么放在 Go 而不是前端 fetch：
//  1. 浏览器发会带 Origin: http://127.0.0.1:7879，部分企业微信/钉钉网关
//     会因为来源不明直接拒收；
//  2. App 切后台时 WebView 的 fetch 会被系统掐断，推送就丢了；
//  3. 报文格式与脱敏逻辑应该只有一份。
//
// 前端只负责把「这次签了什么」汇总成结构体 POST 上来。
type notifier struct {
	mu   sync.Mutex
	path string
	cfg  NotifyConfig
	// failStreak 连续失败次数：达到 3 次在设置页挂黄色横幅提示用户去查地址
	failStreak int
	lastErr    string
	// disabled 因 4xx（地址填错）被自动停用的标记，避免继续无效重试
	disabled bool
}

// NotifyConfig 是通知配置，落盘为 notify_memo.json。
//
// on_start 默认 false：0.5.5 起「打开 App 就跑完整一键签到」，
// 若默认开启，用户一天开 5 次 App 就收 5 条推送——正是他抱怨过的「请求太多」。
type NotifyConfig struct {
	Enabled   bool   `json:"enabled"`
	Type      string `json:"type"` // wecom | dingtalk | feishu | bark | custom
	URL       string `json:"url"`
	OnSuccess bool   `json:"on_success"`
	OnFailure bool   `json:"on_failure"`
	OnManual  bool   `json:"on_manual"`
	OnStart   bool   `json:"on_start"`
	// Always 是「哪怕这一轮没有需要签的账号也推一条」。
	//
	// 与 OnSuccess 的区别要说清楚，否则用户会设错：
	//   OnSuccess —— 有账号签到成功时才推；
	//   Always    —— 一轮跑完就推，无论结果如何（含「全部已签」「全部失败」）。
	// 用户要的是前者时不一定需要后者；两者是独立开关，任一满足即推。
	Always bool `json:"always"`
}

// NotifyAccountLine 是一条账号结果，供摘要渲染。
type NotifyAccountLine struct {
	Provider string  `json:"provider"`
	Label    string  `json:"label"`
	Status   string  `json:"status"` // ok | already | fail
	Message  string  `json:"message"`
	Gain     float64 `json:"gain"`
	Unit     string  `json:"unit"`
}

// NotifyPayload 是前端 POST /api/notify 的请求体。
//
// 汇总在 JS 里算——因为「签了几个、领了几个任务、抽了几次奖」这些数据
// 本来就在前端 stat 对象里，Go 侧并不知道三段流程的明细。
type NotifyPayload struct {
	Trigger  string              `json:"trigger"` // manual | start | single
	Accounts []NotifyAccountLine `json:"accounts"`
	// Summary 是前端已经算好的那句话（"新签 3，已签 1，失败 1｜任务 2 个 +50 灵石"）。
	// 传空时后端自己按 Accounts 拼一个。
	Summary string `json:"summary"`
}

// notifyTypes 是支持的五种渠道。报文格式照 aipy-checkin 线上验证过的写法，
// 不要「顺手规范化」——企业微信和钉钉的字段名不一样，飞书又是另一套。
var notifyTypes = []string{"wecom", "dingtalk", "feishu", "bark", "custom"}

func newNotifier(dir string) *notifier {
	n := &notifier{
		cfg: NotifyConfig{
			Type: "wecom", OnSuccess: true, OnFailure: true, OnManual: true, OnStart: false,
		},
	}
	if dir == "" {
		return n
	}
	n.path = filepath.Join(dir, "notify_memo.json")
	if b, err := os.ReadFile(n.path); err == nil {
		var c NotifyConfig
		if json.Unmarshal(b, &c) == nil {
			n.cfg = c
		}
	}
	if n.cfg.Type == "" {
		n.cfg.Type = "wecom"
	}
	return n
}

func (n *notifier) config() NotifyConfig {
	if n == nil {
		return NotifyConfig{}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cfg
}

// status 返回配置 + 运行态，供设置页渲染。
func (n *notifier) status() map[string]any {
	if n == nil {
		return map[string]any{"enabled": false}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return map[string]any{
		"config":      n.cfg,
		"types":       notifyTypes,
		"fail_streak": n.failStreak,
		"last_error":  n.lastErr,
		"disabled":    n.disabled,
	}
}

// save 保存配置。URL 为空但开关打开时不报错，只在发送时静默跳过。
func (n *notifier) save(c NotifyConfig) error {
	if n == nil {
		return fmt.Errorf("通知模块未就绪")
	}
	c.Type = strings.TrimSpace(strings.ToLower(c.Type))
	if c.Type == "" {
		c.Type = "wecom"
	}
	valid := false
	for _, t := range notifyTypes {
		if t == c.Type {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("不支持的渠道: %s", c.Type)
	}
	c.URL = strings.TrimSpace(c.URL)
	if c.URL != "" && !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") {
		return fmt.Errorf("Webhook 地址必须以 http:// 或 https:// 开头")
	}
	n.mu.Lock()
	n.cfg = c
	// 用户改了地址就重置失败计数，否则改了地址横幅还挂着
	n.failStreak, n.lastErr, n.disabled = 0, "", false
	path := n.path
	n.mu.Unlock()

	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	atomicWrite(path, b)
	return nil
}

// buildText 把 payload 渲染成推送正文。
//
// 两条硬规则：
//  1. 失败必须置顶——手机上一屏放不下 6 个账号，失败混在中间要滑才看得见；
//  2. 账号名用 label 而不是 account_id——account_id 可能是 hash 或手机号。
func buildText(p NotifyPayload) (title, body string) {
	var fails, oks, alreadys []NotifyAccountLine
	for _, a := range p.Accounts {
		switch a.Status {
		case "fail":
			fails = append(fails, a)
		case "already":
			alreadys = append(alreadys, a)
		default:
			oks = append(oks, a)
		}
	}
	title = "积分矩阵 · 签到结果"
	switch p.Trigger {
	case "manual":
		title = "积分矩阵 · 一键签到"
	case "start":
		title = "积分矩阵 · 启动签到"
	case "single":
		title = "积分矩阵 · 单账号签到"
	}

	// 正文里不再重复标题：各渠道报文都是「标题 + 正文」拼起来的，
	// 这里再写一遍用户看到的就是「积分矩阵 · 一键签到」连着出现两次。
	var lines []string
	lines = append(lines, time.Now().Format("2006-01-02 15:04"), "")
	render := func(flag string, list []NotifyAccountLine) {
		for _, a := range list {
			name := a.Label
			if name == "" {
				name = a.Provider
			}
			txt := fmt.Sprintf("%s %s：%s", flag, name, a.Message)
			if a.Gain > 0 {
				unit := a.Unit
				if unit == "" {
					unit = "积分"
				}
				txt += fmt.Sprintf(" +%s %s", fmtNumPlain(a.Gain), unit)
			}
			lines = append(lines, txt)
		}
	}
	render("❌", fails)
	render("✅", oks)
	render("⏭️", alreadys)

	if p.Summary != "" {
		lines = append(lines, "", p.Summary)
	} else {
		lines = append(lines, "", fmt.Sprintf("新签 %d，已签 %d，失败 %d",
			len(oks), len(alreadys), len(fails)))
	}
	return title, strings.Join(lines, "\n")
}

// payload 生成各渠道的请求体。五种格式各不相同，照抄线上验证过的写法。
func notifyPayload(kind, title, text string) []byte {
	var b []byte
	switch kind {
	case "dingtalk":
		b, _ = json.Marshal(map[string]any{
			"msgtype": "text", "text": map[string]string{"content": title + "\n" + text},
		})
	case "feishu":
		b, _ = json.Marshal(map[string]any{
			"msg_type": "text", "content": map[string]string{"text": title + "\n" + text},
		})
	case "bark":
		b, _ = json.Marshal(map[string]string{"title": title, "body": text})
	case "custom":
		b, _ = json.Marshal(map[string]string{"title": title, "text": title + "\n" + text, "content": text})
	default: // wecom
		b, _ = json.Marshal(map[string]any{
			"msgtype": "text", "text": map[string]string{"content": title + "\n" + text},
		})
	}
	return b
}

// send 发一次推送。返回 error 只用于界面提示，调用方（signin 流程）
// 绝不因为它失败而改签到结果——推送是附加价值。
func (n *notifier) send(ctx context.Context, p NotifyPayload, force bool) error {
	if n == nil {
		return fmt.Errorf("通知模块未就绪")
	}
	n.mu.Lock()
	cfg := n.cfg
	disabled := n.disabled
	n.mu.Unlock()

	if !force {
		if !cfg.Enabled || cfg.URL == "" || disabled {
			// 未配置 / 已被停用：静默跳过，不打扰用户
			return errSkipf("通知未启用或已被关闭")
		}
		hasFail := false
		for _, a := range p.Accounts {
			if a.Status == "fail" {
				hasFail = true
			}
		}
		// 按触发源过滤：启动签到默认不推，避免一天开几次 App 收几条
		if p.Trigger == "start" && !cfg.OnStart {
			// 0.6.1：这条以前只是静默跳过，用户在 iOS 上完全不知道自己
			// 「平时打开 App 那一轮」被这个开关挡掉了——他看到的现象是
			// 「发测试收得到，正常签到收不到」。把原因带回前端，设置页才解释得清。
			return errSkipf("未开启「打开 App 自动签到后」推送")
		}
		if p.Trigger == "manual" && !cfg.OnManual {
			return errSkipf("未开启「手动签到后推送」")
		}
		// Always 一票否决：用户明确说「没事也要告诉我」时不再看结果类型
		if !cfg.Always {
			if hasFail && !cfg.OnFailure {
				return errSkipf("未开启「失败时推送」")
			}
			if !hasFail && !cfg.OnSuccess {
				return errSkipf("未开启「成功时推送」")
			}
		}
	}

	title, body := buildText(p)
	// 强制脱敏：账号名里可能混进手机号/邮箱，摘要里可能有 URL
	title = maskNotify(title)
	body = maskNotify(body)
	payload := notifyPayload(cfg.Type, title, body)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return n.noteFailure("构造请求失败: " + err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return n.noteFailure("发送失败: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		// 4xx 基本是地址填错/被吊销：继续发只会每天失败一次，直接自动停用
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			n.mu.Lock()
			n.disabled = true
			n.cfg.Enabled = false
			n.lastErr = fmt.Sprintf("Webhook 返回 HTTP %d，已自动关闭通知", resp.StatusCode)
			path := n.path
			cfg := n.cfg
			n.mu.Unlock()
			if path != "" {
				if b, e := json.MarshalIndent(cfg, "", "  "); e == nil {
					atomicWrite(path, b)
				}
			}
			return fmt.Errorf("%s", n.lastError())
		}
		return n.noteFailure(fmt.Sprintf("Webhook 返回 HTTP %d", resp.StatusCode))
	}
	n.mu.Lock()
	n.failStreak, n.lastErr = 0, ""
	n.mu.Unlock()
	return nil
}

func (n *notifier) lastError() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.lastErr
}

// errSkip 表示「按配置主动跳过，不是失败」。
//
// 为什么必须和真错误区分开：/api/notify 原先只看 err == nil 就回 sent:true，
// 于是被 on_start=false 过滤掉的那一轮也报「已发送」，用户以为收到过。
// 现在跳过会带上原因，前端把它当「没发」处理，也不计入失败计数。
type skipError struct{ reason string }

func (e *skipError) Error() string { return e.reason }

func errSkipf(reason string) error { return &skipError{reason: reason} }

// isSkip 判断是否属于「按配置跳过」。
func isSkip(err error) bool {
	var s *skipError
	return errors.As(err, &s)
}

// noteFailure 累加失败次数，满 3 次让设置页显示横幅。
func (n *notifier) noteFailure(msg string) error {
	n.mu.Lock()
	n.failStreak++
	n.lastErr = msg
	n.mu.Unlock()
	return fmt.Errorf("%s", msg)
}

// maskNotify 对推送正文做脱敏。
//
// 为什么必须在发送前做：Webhook 是把内容推出设备，一旦里面有 cookie 或
// 完整 key，等于把凭据发到了第三方服务器（企业微信也会留档）。
// 规则与 httpx.maskText 一致，但这里再兜一遍，避免调用方漏字段。
func maskNotify(s string) string {
	if s == "" {
		return s
	}
	// sk- 开头的密钥
	s = maskPattern(s, "sk-")
	// Cookie / Authorization 头
	s = maskAfter(s, "cookie:", 80)
	s = maskAfter(s, "cookie=", 80)
	s = maskAfter(s, "session=", 80)
	s = maskAfter(s, "session:", 80)
	s = maskAfter(s, "Authorization:", 80)
	s = maskAfter(s, "authorization:", 80)
	s = maskAfter(s, "Bearer ", 60)
	s = maskAfter(s, "bearer ", 60)
	// 手机号（11 位数字）与邮箱
	s = maskPhone(s)
	s = maskEmail(s)
	return s
}

// maskPattern 把 prefix 之后连续的非空白字符打码。
func maskPattern(s, prefix string) string {
	lower := strings.ToLower(s)
	out := s
	from := 0
	for {
		i := strings.Index(lower[from:], prefix)
		if i < 0 {
			return out
		}
		i += from
		j := i + len(prefix)
		for j < len(out) && !isSpaceOrPunct(out[j]) {
			j++
		}
		if j > i+len(prefix) {
			out = out[:i] + prefix + "***" + out[j:]
			lower = strings.ToLower(out)
		}
		from = i + len(prefix) + 3
		if from >= len(out) {
			return out
		}
	}
}

// maskAfter 把 key 之后 n 个字符内的内容清空（用于 header 片段）。
func maskAfter(s, key string, n int) string {
	lower := strings.ToLower(s)
	i := strings.Index(lower, key)
	if i < 0 {
		return s
	}
	start := i + len(key)
	end := start
	for end < len(s) && end-start < n && s[end] != '\n' && s[end] != ' ' {
		end++
	}
	if end <= start {
		return s
	}
	return s[:start] + " ***" + s[end:]
}

func maskPhone(s string) string {
	out := []byte(s)
	for i := 0; i+11 <= len(out); i++ {
		allDigit := true
		for j := i; j < i+11; j++ {
			if out[j] < '0' || out[j] > '9' {
				allDigit = false
				break
			}
		}
		// 左右不能紧邻其它数字，否则会把长数字串切错
		if allDigit && (i == 0 || out[i-1] < '0' || out[i-1] > '9') &&
			(i+11 == len(out) || out[i+11] < '0' || out[i+11] > '9') {
			copy(out[i:], []byte("***********"))
			i += 10
		}
	}
	return string(out)
}

func maskEmail(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\n' || r == ',' || r == ';'
	})
	out := s
	for _, p := range parts {
		if !strings.Contains(p, "@") || strings.Contains(p, "***") {
			continue
		}
		at := strings.Index(p, "@")
		if at <= 0 || at == len(p)-1 {
			continue
		}
		out = strings.ReplaceAll(out, p, p[:1]+"***"+p[at:])
	}
	return out
}

func isSpaceOrPunct(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '"', '\'', ',', ';', ')', ']', '}':
		return true
	}
	return false
}

// fmtNumPlain 数字转字符串，去掉浮点尾巴（1.5→"1.5"，2→"2"，0.04→"0.04"）。
func fmtNumPlain(f float64) string {
	s := fmt.Sprintf("%.4f", f)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}
