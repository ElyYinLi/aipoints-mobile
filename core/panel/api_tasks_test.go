package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
	"github.com/ElyYinLi/aipoints-mobile/core/provider"
	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// fakeStoreWithAccounts 返回固定账号列表，供 query/tasks 测试。
type fakeStoreWithAccounts struct {
	accounts []store.Account
}

func (f fakeStoreWithAccounts) List() ([]store.Account, error) { return f.accounts, nil }
func (f fakeStoreWithAccounts) Put(store.Account) error        { return nil }
func (f fakeStoreWithAccounts) Delete(p, a string) error       { return nil }

// TestProvidersMarkTaskSupport 只有 AiPy 支持任务领取。
func TestProvidersMarkTaskSupport(t *testing.T) {
	reg := provider.NewRegistry(httpx.New())
	s := New(Options{Store: fakeStore{}, Reg: reg})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/providers", s.handleProviders)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)

	var aipyTasks, othersTasks bool
	for _, p := range list {
		tasks, _ := p["tasks"].(bool)
		if p["id"] == "aipy" {
			aipyTasks = tasks
		} else if tasks {
			othersTasks = true
		}
	}
	if !aipyTasks {
		t.Fatalf("aipy 应标记 tasks=true，实际 %v", list)
	}
	if othersTasks {
		t.Fatalf("其他平台不应标记 tasks=true")
	}
}

// TestQueryBackfillsLabel 查询结果要带回账号备注。
func TestQueryBackfillsLabel(t *testing.T) { // 假上游：aipy 返回正常快照
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/checkin/status") {
			w.Write([]byte(`{"success":true,"data":{"checked_in_today":true}}`))
		} else {
			w.Write([]byte(`{"success":true,"data":{"quota":5000}}`))
		}
	}))
	defer up.Close()

	reg := provider.NewRegistry(httpx.New())
	fs := fakeStoreWithAccounts{accounts: []store.Account{{
		Provider:   "aipy",
		AccountID:  "abc123",
		Label:      "主号",
		Enabled:    true,
		SecretJSON: json.RawMessage(`{"cookie":"s=1","domain":"` + up.URL + `"}`),
	}}}
	s := New(Options{Store: fs, Reg: reg})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/query", s.handleQuery)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/query")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 1 {
		t.Fatalf("应返回 1 个账号，实际 %d", len(out))
	}
	if out[0]["label"] != "主号" {
		t.Fatalf("label 应回填为「主号」，实际 %v", out[0]["label"])
	}
}

// TestTaskClaimAll 一键领取：只领 claimable 的任务，逐个写请求，汇总结果。
func TestTaskClaimAll(t *testing.T) {
	var claimedTypes []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/user/daily_tasks/status":
			w.Write([]byte(`{"success":true,"data":[
				{"task_type":"chat","title":"对话","reward":50000,"progress":1,"target":1,"status":"locked"},
				{"task_type":"share","title":"分享","reward":100000,"progress":0,"target":3,"status":"locked"},
				{"task_type":"daily","title":"登录","reward":20000,"progress":1,"target":1,"status":"claimed"}
			]}`))
		case r.URL.Path == "/api/user/daily_tasks/claim" && r.Method == "POST":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			claimedTypes = append(claimedTypes, in["task_type"])
			w.Write([]byte(`{"success":true,"data":{"reward":50000}}`))
		default:
			w.Write([]byte(`{"success":true,"data":{}}`))
		}
	}))
	defer up.Close()

	fs := fakeStoreWithAccounts{accounts: []store.Account{
		{Provider: "aipy", AccountID: "a1", Enabled: true,
			SecretJSON: json.RawMessage(`{"cookie":"s=1","domain":"` + up.URL + `"}`)},
		// 停用账号不参与
		{Provider: "aipy", AccountID: "a2", Enabled: false,
			SecretJSON: json.RawMessage(`{"cookie":"s=2","domain":"` + up.URL + `"}`)},
		// 不支持任务的平台被跳过
		{Provider: "lingxi", AccountID: "l1", Enabled: true,
			SecretJSON: json.RawMessage(`{"cookie":"s=3"}`)},
	}}
	s := New(Options{Store: fs, Reg: provider.NewRegistry(httpx.New())})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/claim_all", s.handleTaskClaimAll)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/tasks/claim_all", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	results, _ := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("应只有 aipy/a1 参与，实际 %d 个", len(results))
	}
	row := results[0].(map[string]any)
	if row["claimed"].(float64) != 1 {
		t.Fatalf("应领取 1 个任务（chat 达标、share 未达标、daily 已领），实际 %v", row["claimed"])
	}
	if row["gain"].(float64) != 10 {
		t.Fatalf("gain 应为 10 灵石（50000/5000），实际 %v", row["gain"])
	}
	if len(claimedTypes) != 1 || claimedTypes[0] != "chat" {
		t.Fatalf("只应对 chat 发领取请求，实际 %v", claimedTypes)
	}
}
