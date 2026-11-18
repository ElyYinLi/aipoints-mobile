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
	if !strings.Contains(snap.Detail, "10 灵石") {
		t.Fatalf("已签文案应带奖励: %q", snap.Detail)
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
