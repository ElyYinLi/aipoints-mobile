package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// uuidRe 是百度 BCE 网关认的 request_id 形状：36 字符，8-4-4-4-12 带连字符。
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// TestRandUUIDShape 锁死 UUID 格式。
//
// 这是 410110 的回归测试：早期用 randHex(16) 生成 32 位纯 hex，
// 真人点「立即抽奖」直接被网关拒：{"code":410110,"msg":"抽奖请求ID无效"}。
func TestRandUUIDShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		u := randUUID()
		if !uuidRe.MatchString(u) {
			t.Fatalf("request_id 必须是标准 UUID，实际 %q", u)
		}
		// 版本位必须是 4，变体位必须是 8/9/a/b（RFC 4122 v4）
		if u[14] != '4' {
			t.Fatalf("UUID 版本位应为 4，实际 %q", u)
		}
		if v := u[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
			t.Fatalf("UUID 变体位非法: %q", u)
		}
		if seen[u] {
			t.Fatalf("UUID 重复: %q", u)
		}
		seen[u] = true
	}
}

// TestDumateDrawRequestIDIsUUID 证明抽奖请求体里的 request_id 是合法 UUID。
func TestDumateDrawRequestIDIsUUID(t *testing.T) {
	var bodyRID, hdrRID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dumate/activity/growth-plan/draw":
			hdrRID = r.Header.Get("x-bce-request-id")
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			bodyRID, _ = body["request_id"].(string)
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"prize_name": "50 积分红包", "prize_points": 50, "remaining_draws": 0,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewDumate(httpx.New())
	res, err := p.Draw(context.Background(), Credential{Cookie: "BDUSS=1", Domain: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("抽奖应成功: %+v", res)
	}
	if !uuidRe.MatchString(bodyRID) {
		t.Fatalf("请求体 request_id 必须是标准 UUID，实际 %q", bodyRID)
	}
	if !uuidRe.MatchString(hdrRID) {
		t.Fatalf("x-bce-request-id 也应是 UUID 形状，实际 %q", hdrRID)
	}
	// 两个 ID 必须不同：复用同一个值时服务端会判重复请求
	if bodyRID == hdrRID {
		t.Fatalf("x-bce-request-id 与 request_id 不应相同: %q", bodyRID)
	}
	if res.Gain != 50 || res.Prize != "50 积分红包" {
		t.Fatalf("奖品解析错误: %+v", res)
	}
}

// TestDumateCheckinReportsGain 签到成功后必须回带上账积分。
//
// 领积分接口常常只回 {"code":0,"msg":"ok"} 不回数值，此时用「签到前后总额差值」反推，
// 这样提示才能显示「签到成功，+50 积分」而不是光秃秃一句「签到成功」。
func TestDumateCheckinReportsGain(t *testing.T) {
	var total atomic.Int64
	total.Store(1000)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dumate/points/loginBonusInfo":
			// 签到前 totalPoints=1000 且未签；签到后 hasIssued=true 且 totalPoints=1050
			issued := total.Load() > 1000
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"hasIssued": issued, "totalPoints": total.Load(),
			}})
		case "/api/dumate/points/quota_overview":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"totalPoints": total.Load(), "usedPoints": 0,
			}})
		case "/api/dumate/points/loginBonus":
			total.Add(50) // 领到 50 积分
			writeJSON(w, map[string]any{"code": 0, "msg": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewDumate(httpx.New())
	res, _ := p.Checkin(context.Background(), Credential{Cookie: "BDUSS=1", Domain: srv.URL})
	if !res.OK || res.Already {
		t.Fatalf("应签到成功: %+v", res)
	}
	if res.Gain != 50 {
		t.Fatalf("应通过总额差值反推出 +50，实际 %+v", res)
	}
	if res.Unit != "积分" {
		t.Fatalf("签到结果必须带单位「积分」，实际 %q", res.Unit)
	}
}

// TestDumateAlreadyCheckedInReportsGain 已签时也要带今日积分（今日已签 +N 积分）。
func TestDumateAlreadyCheckedInReportsGain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dumate/points/loginBonusInfo":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"hasIssued": true, "totalPoints": 120, "todayPoints": 20,
			}})
		case "/api/dumate/points/quota_overview":
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"totalPoints": 120, "usedPoints": 0}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewDumate(httpx.New())
	res, _ := p.Checkin(context.Background(), Credential{Cookie: "BDUSS=1", Domain: srv.URL})
	if !res.OK || !res.Already {
		t.Fatalf("应为已签: %+v", res)
	}
	if res.Gain != 20 {
		t.Fatalf("已签时应回带今日 +20，实际 %+v", res)
	}
}

// TestWorkBuddyCheckinHasUnit WorkBuddy 的提示必须带「积分」二字。
func TestWorkBuddyCheckinHasUnit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"gain": 100}})
	}))
	defer srv.Close()

	p := NewWorkBuddy(httpx.New())
	res, _ := p.Checkin(context.Background(), Credential{AccessToken: "t", Domain: srv.URL})
	if !res.OK || res.Gain != 100 {
		t.Fatalf("签到应成功 +100: %+v", res)
	}
	if res.Unit != "积分" {
		t.Fatalf("单位必须为「积分」，实际 %q", res.Unit)
	}
}

// TestDumateDraw410110 网关仍拒绝时要给出可操作提示，而不是干巴巴的业务码。
func TestDumateDraw410110(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 410110, "msg": "抽奖请求ID无效"})
	}))
	defer srv.Close()

	p := NewDumate(httpx.New())
	res, _ := p.Draw(context.Background(), Credential{Cookie: "BDUSS=1", Domain: srv.URL})
	if res.OK {
		t.Fatalf("410110 不应判成功: %+v", res)
	}
	if !strings.Contains(res.Error, "运行日志") {
		t.Fatalf("应引导用户去看运行日志，实际 %q", res.Error)
	}
}
