package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// dailyMemo 是 0.6.4 启动判定的唯一依据：今天跑过就不能再签。
// 这条逻辑错了就会「一天签好几轮」——正是用户最在意的黑号风险。
func TestDailyMemoLifecycle(t *testing.T) {
	dir := t.TempDir()
	m := newDailyMemo(dir)

	if _, ok := m.Today(); ok {
		t.Fatal("新目录不应有今天的记录")
	}

	m.Begin("ab12", "start")
	e, ok := m.Today()
	if !ok {
		t.Fatal("Begin 之后必须有今天的记录")
	}
	if e.Done {
		t.Fatal("刚 Begin 不能算完成：中途被杀要允许补跑")
	}
	if e.Round != "ab12" || e.Trigger != "start" {
		t.Fatalf("round/trigger 没写对：%+v", e)
	}

	m.SetAccount("aipy", "u1", "ok")
	m.SetAccount("dumate", "u2", "fail")
	m.Finish()

	e, ok = m.Today()
	if !ok || !e.Done {
		t.Fatal("Finish 之后 Done 必须为 true")
	}
	if e.Accounts["aipy|u1"] != "ok" || e.Accounts["dumate|u2"] != "fail" {
		t.Fatalf("逐账号结果丢失：%v", e.Accounts)
	}

	// 重启进程（新实例读同一个目录）后必须还能读到——这是「开十次 App 只签一次」的前提
	m2 := newDailyMemo(dir)
	e2, ok := m2.Today()
	if !ok || !e2.Done || e2.Accounts["aipy|u1"] != "ok" {
		t.Fatalf("重启后记录丢失：%+v", e2)
	}
}

// 昨天的记录绝不能算「今天跑过」，否则第二天整天都不会签到。
func TestDailyMemoIgnoresYesterday(t *testing.T) {
	m := newDailyMemo("")
	y := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	m.data[y] = dailyEntry{Date: y, Done: true}
	if _, ok := m.Today(); ok {
		t.Fatal("昨天的记录被当成了今天，会导致次日完全不签到")
	}
}

// /api/daily 的 ran 语义：只有「今天且走完」才算跑过。
// done=false（半路被杀）必须返回 ran=false，让前端补跑一次。
func TestHandleDailyRanSemantics(t *testing.T) {
	s := New(Options{Store: fakeStore{}, DataDir: t.TempDir()})

	get := func() map[string]any {
		rec := httptest.NewRecorder()
		s.handleDaily(rec, httptest.NewRequest(http.MethodGet, "/api/daily", nil))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	if v := get(); v["ran"] != false {
		t.Fatal("没有记录时 ran 必须为 false")
	}

	s.daily.Begin("cd34", "start")
	if v := get(); v["ran"] != false {
		t.Fatal("只 Begin 没 Finish 时 ran 必须为 false（半路被杀要允许补跑）")
	}

	s.daily.Finish()
	v := get()
	if v["ran"] != true {
		t.Fatalf("走完一轮后 ran 必须为 true，实际 %v", v["ran"])
	}
	if v["round"] != "cd34" {
		t.Fatalf("round 没带出来：%v", v["round"])
	}
}

// mark 接口的三个 phase 必须都能落盘；未知 phase 要明确报错而不是静默吞掉。
func TestHandleDailyMarkPhases(t *testing.T) {
	s := New(Options{Store: fakeStore{}, DataDir: t.TempDir()})

	do := func(body string) map[string]any {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/daily/mark", strings.NewReader(body))
		s.handleDailyMark(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	if v := do(`{"phase":"begin","round":"e1f2","trigger":"manual"}`); v["ok"] != true {
		t.Fatalf("begin 失败：%v", v)
	}
	if v := do(`{"phase":"account","round":"e1f2","provider":"aipy","account":"u9","status":"ok"}`); v["ok"] != true {
		t.Fatalf("account 失败：%v", v)
	}
	if v := do(`{"phase":"finish","round":"e1f2"}`); v["ok"] != true {
		t.Fatalf("finish 失败：%v", v)
	}
	e, ok := s.daily.Today()
	if !ok || !e.Done || e.Accounts["aipy|u9"] != "ok" || e.Trigger != "manual" {
		t.Fatalf("打点没落全：%+v", e)
	}
	if v := do(`{"phase":"nope"}`); v["ok"] != false {
		t.Fatal("未知 phase 必须报错")
	}
}

// 前端日志入口必须脱敏：用户会把日志整段贴到聊天里求助。
func TestHandleLogMasksCredential(t *testing.T) {
	s := New(Options{Store: fakeStore{}, DataDir: t.TempDir()})
	rec := httptest.NewRecorder()
	body := `{"level":"warn","scope":"app","msg":"cookie=BDUSS=abcdef1234567890 出错了"}`
	s.handleLog(rec, httptest.NewRequest(http.MethodPost, "/api/log", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}

	var hit string
	for _, e := range httpx.Logs(0) {
		if strings.Contains(e.Msg, "出错了") {
			hit = e.Msg
		}
	}
	if hit == "" {
		t.Fatal("前端日志没有写进缓冲")
	}
	if strings.Contains(hit, "abcdef1234567890") {
		t.Fatal("凭据没被脱敏，日志能直接泄露到聊天里")
	}
}
