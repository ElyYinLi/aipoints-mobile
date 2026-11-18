package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
	"github.com/ElyYinLi/aipoints-mobile/core/provider"
	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// TestLotteryDrawAll 一键抽奖：按剩余次数逐次抽，非抽奖平台跳过，停用账号跳过。
func TestLotteryDrawAll(t *testing.T) {
	var drawCalls int32
	var lastBody map[string]any

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dumate/activity/growth-plan/draw/status":
			w.Write([]byte(`{"code":0,"data":{"remaining_draws":2}}`))
		case "/api/dumate/activity/growth-plan/draw":
			n := atomic.AddInt32(&drawCalls, 1)
			lastBody = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&lastBody)
			if n == 1 {
				w.Write([]byte(`{"code":0,"data":{"remaining_draws":1,"prize_name":"50积分","prize_points":50}}`))
			} else {
				w.Write([]byte(`{"code":0,"data":{"remaining_draws":0,"prize_name":"谢谢参与"}}`))
			}
		default:
			w.Write([]byte(`{"code":0}`))
		}
	}))
	defer up.Close()

	fs := fakeStoreWithAccounts{accounts: []store.Account{
		// 支持抽奖，剩余 2 次
		{Provider: "dumate", AccountID: "d1", Enabled: true,
			SecretJSON: json.RawMessage(`{"cookie":"BDUSS=x","domain":"` + up.URL + `"}`)},
		// 停用账号不参与
		{Provider: "dumate", AccountID: "d2", Enabled: false,
			SecretJSON: json.RawMessage(`{"cookie":"BDUSS=y","domain":"` + up.URL + `"}`)},
		// 不支持抽奖的平台被跳过
		{Provider: "lingxi", AccountID: "l1", Enabled: true,
			SecretJSON: json.RawMessage(`{"cookie":"s=3"}`)},
	}}
	s := New(Options{Store: fs, Reg: provider.NewRegistry(httpx.New())})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/lottery/draw_all", s.handleLotteryDrawAll)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/lottery/draw_all", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)

	results, _ := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("应只有 dumate/d1 参与，实际 %d 个：%v", len(results), out)
	}
	row := results[0].(map[string]any)
	if row["drawn"].(float64) != 2 {
		t.Fatalf("应按剩余次数抽 2 次，实际 %v", row["drawn"])
	}
	if row["gain"].(float64) != 50 {
		t.Fatalf("积分奖品应累加 50，实际 %v", row["gain"])
	}
	if int(atomic.LoadInt32(&drawCalls)) != 2 {
		t.Fatalf("上游 draw 应被调用 2 次，实际 %d", drawCalls)
	}
	// 幂等键必须存在且非空——复用同一个值第二次会被上游判重复
	if rid, ok := lastBody["request_id"].(string); !ok || rid == "" {
		t.Fatalf("draw 必须带非空 request_id，实际 %v", lastBody)
	}
}

// TestLotteryDrawAllNoChance 没有抽奖机会时不发抽奖请求、也不报错。
func TestLotteryDrawAllNoChance(t *testing.T) {
	var drawCalls int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/dumate/activity/growth-plan/draw/status" {
			w.Write([]byte(`{"code":0,"data":{"remaining_draws":0}}`))
			return
		}
		if r.URL.Path == "/api/dumate/activity/growth-plan/draw" {
			atomic.AddInt32(&drawCalls, 1)
		}
		w.Write([]byte(`{"code":0}`))
	}))
	defer up.Close()

	fs := fakeStoreWithAccounts{accounts: []store.Account{
		{Provider: "dumate", AccountID: "d1", Enabled: true,
			SecretJSON: json.RawMessage(`{"cookie":"BDUSS=x","domain":"` + up.URL + `"}`)},
	}}
	s := New(Options{Store: fs, Reg: provider.NewRegistry(httpx.New())})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/lottery/draw_all", s.handleLotteryDrawAll)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/lottery/draw_all", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	results, _ := out["results"].([]any)
	if len(results) != 0 {
		t.Fatalf("没次数时不应产生结果行，实际 %v", results)
	}
	if int(atomic.LoadInt32(&drawCalls)) != 0 {
		t.Fatalf("没次数时不应调用 draw，实际 %d 次", drawCalls)
	}
}

// TestProvidersMarkLotterySupport 只有百度搭子标记 lottery=true。
func TestProvidersMarkLotterySupport(t *testing.T) {
	reg := provider.NewRegistry(httpx.New())
	var dumate, others bool
	for _, m := range reg.All() {
		if m.ID == "dumate" {
			dumate = m.Lottery
		} else if m.Lottery {
			others = true
		}
	}
	if !dumate {
		t.Fatal("dumate 应标记 lottery=true")
	}
	if others {
		t.Fatal("其他平台不应标记 lottery=true")
	}
}
