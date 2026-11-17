package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// TestWorkBuddyQueryAndCheckin 用假服务器验证请求形状（方法/路径/身份头）与响应解析。
// 价值：把接口契约固化下来，官方接口一变测试先红。
func TestWorkBuddyQueryAndCheckin(t *testing.T) {
	var seen = map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen["path"] = r.URL.Path
		seen["method"] = r.Method
		seen["auth"] = r.Header.Get("Authorization")
		seen["uid"] = r.Header.Get("X-User-Id")
		seen["origin"] = r.Header.Get("Origin")
		seen["ua"] = r.Header.Get("User-Agent")
		switch r.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"checked_in": true, "streak": 7, "total_credits": 300,
			}})
		case "/billing/meter/get-user-resource-summary":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"Packages": []any{
					map[string]any{"CycleRemainCapacity": 1200.5},
					map[string]any{"CycleRemainCapacity": 300.0},
				},
			}})
		case "/v2/billing/meter/daily-checkin":
			writeJSON(w, map[string]any{"code": 10001, "msg": "今日已签到"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewWorkBuddy(httpx.New())
	cred := Credential{AccountID: "a1", AccessToken: "tok", UID: "u1", EnterpriseID: "e1", Domain: srv.URL}

	snap, err := p.Query(context.Background(), cred)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.OK || !snap.CheckedIn {
		t.Fatalf("期望已签到，实际 %+v", snap)
	}
	if snap.Streak != 7 {
		t.Fatalf("streak 期望 7，实际 %d", snap.Streak)
	}
	// 余额接口（无 /v2）必须被调用并求和
	if snap.Balance != 1500.5 {
		t.Fatalf("余额期望 1500.5，实际 %v", snap.Balance)
	}
	if seen["auth"] != "Bearer tok" {
		t.Fatalf("Authorization 头错误: %q", seen["auth"])
	}
	if seen["uid"] != "u1" {
		t.Fatalf("X-User-Id 头错误: %q", seen["uid"])
	}
	if !strings.Contains(seen["ua"], "Mozilla") {
		t.Fatal("必须带浏览器 User-Agent（否则余额接口 403）")
	}

	res, err := p.Checkin(context.Background(), cred)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Already {
		t.Fatalf("业务码 10001 应判定为已签到，实际 %+v", res)
	}
}

// TestLingxiFlow 验证 tasks 解析 + claim 路径带 date 参数。
func TestLingxiFlow(t *testing.T) {
	var claimPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/public/v1/tasks":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"tasks": []any{
				map[string]any{"type": "weekly", "status": "available"},
				map[string]any{"type": "daily", "status": "available",
					"extra": map[string]any{"today_day": 3}},
			}}})
		case r.URL.Path == "/api/public/v1/credits/balance":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"balance": 5800}})
		case strings.HasPrefix(r.URL.Path, "/api/public/v1/tasks/daily_check_in/claim"):
			claimPath = r.URL.String()
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"credits": 100}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewLingxi(httpx.New())
	cred := Credential{AccountID: "k1", Cookie: "SID=1", Domain: srv.URL}

	snap, _ := p.Query(context.Background(), cred)
	if snap.CheckedIn {
		t.Fatal("status=available 时不应判定为已签")
	}
	if snap.Balance != 5800 {
		t.Fatalf("智点余额期望 5800，实际 %v", snap.Balance)
	}
	if snap.Streak != 3 {
		t.Fatalf("today_day 期望 3，实际 %d", snap.Streak)
	}

	res, _ := p.Checkin(context.Background(), cred)
	if !res.OK {
		t.Fatalf("签到应成功: %+v", res)
	}
	if !strings.Contains(claimPath, "date=") {
		t.Fatalf("claim 必须带 date 参数，实际 %q", claimPath)
	}
}

// TestDumateQuota 验证「可用 = 总额 - 已用」与写接口带 x-bce-request-id。
func TestDumateQuota(t *testing.T) {
	var gotReqID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dumate/points/loginBonusInfo":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"signed_today": false}})
		case "/api/dumate/points/quota_overview":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"total_points": 2200, "used_points": 500,
			}})
		case "/api/dumate/points/loginBonus":
			gotReqID = r.Header.Get("x-bce-request-id")
			writeJSON(w, map[string]any{"code": 0, "msg": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewDumate(httpx.New())
	cred := Credential{AccountID: "d1", Cookie: "BDUSS=1", Domain: srv.URL}

	snap, _ := p.Query(context.Background(), cred)
	if snap.Balance != 1700 {
		t.Fatalf("可用积分期望 1700，实际 %v", snap.Balance)
	}
	res, _ := p.Checkin(context.Background(), cred)
	if !res.OK {
		t.Fatalf("签到应成功: %+v", res)
	}
	if len(gotReqID) < 16 {
		t.Fatalf("写接口必须带 x-bce-request-id，实际 %q", gotReqID)
	}
}

// TestRegistryHasCore 确保三家平台都注册成功。
func TestRegistryHasCore(t *testing.T) {
	reg := NewRegistry(httpx.New())
	for _, id := range []string{"workbuddy", "lingxi", "dumate"} {
		if _, ok := reg.Get(id); !ok {
			t.Fatalf("缺少平台 %s", id)
		}
	}
	if len(reg.All()) != 3 {
		t.Fatalf("期望 3 个平台，实际 %d", len(reg.All()))
	}
}

// TestMissingCredentialFailsFast 缺凭据要立刻返回可读错误，不能真发请求。
func TestMissingCredentialFailsFast(t *testing.T) {
	p := NewLingxi(httpx.New())
	snap, err := p.Query(context.Background(), Credential{})
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if snap.Error == "" {
		t.Fatal("缺 cookie 时应有错误说明")
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
