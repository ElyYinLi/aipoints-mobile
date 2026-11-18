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
		if r.URL.Path != "/api/user/checkin/status" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Cookie"), "session=abc") {
			t.Errorf("cookie 未透传: %q", r.Header.Get("Cookie"))
		}
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			t.Error("缺少 X-Requested-With")
		}
		w.Write([]byte(`{"success":true,"data":{"checked_in":true,"continuous_days":7,"quota":1500000}}`))
	})
	defer srv.Close()

	snap, err := p.Query(context.Background(), Credential{Cookie: "session=abc", Domain: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !snap.OK || !snap.CheckedIn || snap.Streak != 7 {
		t.Fatalf("快照不符: %+v", snap)
	}
	if snap.Balance != 3 {
		t.Fatalf("额度换算错误: %v", snap.Balance)
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
		w.Write([]byte(`{"success":true,"message":"签到成功","data":{"reward":500000}}`))
	})
	defer srv.Close()

	res, _ := p.Checkin(context.Background(), Credential{Cookie: "s=1", Domain: srv.URL})
	if !res.OK || res.Already || res.Gain != 1 {
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
