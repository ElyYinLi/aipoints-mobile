package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// newAiPyServer 起一个假的上游，按 path 分发。
func newAiPyServer(t *testing.T, handler http.HandlerFunc) (*AiPy, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	cli := httpx.New()
	return NewAiPy(cli), srv
}

func TestAiPyQueryCheckedIn(t *testing.T) {
	p, srv := newAiPyServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "session=abc") {
			t.Errorf("cookie 未透传: %q", r.Header.Get("Cookie"))
		}
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			t.Error("缺少 X-Requested-With")
		}
		switch r.URL.Path {
		case "/api/user/checkin/status":
			// 字段以 types.go 的 CheckinStatus 为准：已签是 checked_in_today，
			// 写成 checked_in 上游不认（曾导致「签完还显示未签到」）
			w.Write([]byte(`{"success":true,"data":{"checked_in_today":true,"cycle_day":7,"today_total_reward":50000}}`))
		case "/api/user/self":
			// 余额只在这个接口的 User.Quota 里，status 没有这个字段
			w.Write([]byte(`{"success":true,"data":{"quota":1500000,"used_quota":200000}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	defer srv.Close()

	snap, err := p.Query(context.Background(), Credential{Cookie: "session=abc", Domain: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !snap.OK || !snap.CheckedIn || snap.Streak != 7 {
		t.Fatalf("快照不符: %+v", snap)
	}
	// 灵石 = 额度 ÷ 5000
	if snap.Balance != 300 {
		t.Fatalf("灵石换算错误: %v", snap.Balance)
	}
	if snap.Unit != "灵石" {
		t.Fatalf("单位应为灵石，实际 %q", snap.Unit)
	}
	// 今日收益必须以【数值】给出（today_gain），文案由面板 detailText() 统一拼成
	// 「今日已签 +N 灵石」。之前这里拼进 Detail 字符串，面板只认数值 → +N 丢失。
	if snap.TodayGain != 10 {
		t.Fatalf("today_gain 应为 10 灵石，实际 %v（Detail=%q）", snap.TodayGain, snap.Detail)
	}
	if snap.Detail != "今日已签" {
		t.Fatalf("已签状态文案应为「今日已签」，实际 %q", snap.Detail)
	}
}

func TestAiPyQueryAuthFailure(t *testing.T) {
	p, srv := newAiPyServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	})
	defer srv.Close()

	snap, _ := p.Query(context.Background(), Credential{Cookie: "x", Domain: srv.URL})
	if snap.OK || !strings.Contains(snap.Error, "失效") {
		t.Fatalf("应识别为登录失效: %+v", snap)
	}
}

func TestAiPyQueryMissingCred(t *testing.T) {
	p := NewAiPy(httpx.New())
	snap, _ := p.Query(context.Background(), Credential{})
	if snap.OK || snap.Error == "" {
		t.Fatalf("空凭据应直接报错: %+v", snap)
	}
}

func TestAiPyCheckinSuccess(t *testing.T) {
	p, srv := newAiPyServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/user/checkin" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"success":true,"message":"签到成功","data":{"total_reward":50000,"base_reward":40000}}`))
	})
	defer srv.Close()

	res, _ := p.Checkin(context.Background(), Credential{Cookie: "s=1", Domain: srv.URL})
	if !res.OK || res.Already || res.Gain != 10 || res.Unit != "灵石" {
		t.Fatalf("签到结果不符: %+v", res)
	}
}

func TestAiPyCheckinAlready(t *testing.T) {
	p, srv := newAiPyServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":false,"message":"今日已签到，请勿重复签到"}`))
	})
	defer srv.Close()

	res, _ := p.Checkin(context.Background(), Credential{Cookie: "s=1", Domain: srv.URL})
	if !res.OK || !res.Already {
		t.Fatalf("重复签到应判为已签: %+v", res)
	}
}

func TestAiPyCheckinBusinessFail(t *testing.T) {
	p, srv := newAiPyServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":false,"message":"账户被风控"}`))
	})
	defer srv.Close()

	res, _ := p.Checkin(context.Background(), Credential{Cookie: "s=1", Domain: srv.URL})
	if res.OK || !strings.Contains(res.Error, "风控") {
		t.Fatalf("应透出上游错误: %+v", res)
	}
}

func TestAiPyTasksList(t *testing.T) {
	// 上游 data 是数组形状（实测 v1.3.2）
	p, srv := newAiPyServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/daily_tasks/status" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`{"success":true,"data":[
			{"task_type":"chat","title":"完成一次对话","reward":50000,"progress":1,"target":1,"status":"locked"},
			{"task_type":"share","title":"分享给好友","reward":100000,"progress":0,"target":3,"status":"locked"},
			{"task_type":"daily","title":"每日登录","reward":20000,"progress":1,"target":1,"status":"claimed"}
		]}`))
	})
	defer srv.Close()

	tasks, err := p.Tasks(context.Background(), Credential{Cookie: "s=1", Domain: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("应解析出 3 个任务，实际 %d", len(tasks))
	}
	// 第一个：达标但 status=locked（未领取），应可领
	if !tasks[0].Claimable {
		t.Fatalf("chat 任务应可领取: %+v", tasks[0])
	}
	// 灵石换算：50000 / 5000 = 10
	if tasks[0].Reward != 10 || tasks[0].Unit != "灵石" {
		t.Fatalf("chat 奖励换算错误: %+v", tasks[0])
	}
	// 第二个：进度 0/3，未达标，不可领
	if tasks[1].Claimable {
		t.Fatalf("share 任务未达标不应可领: %+v", tasks[1])
	}
	// 第三个：已领取
	if tasks[2].Claimable || tasks[2].Status != "claimed" {
		t.Fatalf("daily 任务应已领取: %+v", tasks[2])
	}
}

func TestAiPyTasksWrapped(t *testing.T) {
	// 兼容 data 是 {tasks:[...]} 的形状
	p, srv := newAiPyServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"tasks":[{"task_type":"a","title":"A","reward":5000,"progress":1,"target":1,"status":"locked"}]}}`))
	})
	defer srv.Close()

	tasks, err := p.Tasks(context.Background(), Credential{Cookie: "s=1", Domain: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].TaskType != "a" {
		t.Fatalf("应解析出 1 个任务: %+v", tasks)
	}
}

func TestAiPyClaimTaskSuccess(t *testing.T) {
	p, srv := newAiPyServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/user/daily_tasks/claim" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"success":true,"message":"领取成功","data":{"reward":50000}}`))
	})
	defer srv.Close()

	res, err := p.ClaimTask(context.Background(), Credential{Cookie: "s=1", Domain: srv.URL}, "chat")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Gain != 10 || res.Unit != "灵石" {
		t.Fatalf("领取结果不符: %+v", res)
	}
}

func TestAiPyClaimTaskNotCompleted(t *testing.T) {
	p, srv := newAiPyServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":false,"message":"任务未完成"}`))
	})
	defer srv.Close()

	res, _ := p.ClaimTask(context.Background(), Credential{Cookie: "s=1", Domain: srv.URL}, "share")
	if res.OK || !strings.Contains(res.Error, "未完成") {
		t.Fatalf("未达标应透出错误: %+v", res)
	}
}
