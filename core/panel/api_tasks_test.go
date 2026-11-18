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
func TestQueryBackfillsLabel(t *testing.T) {
	// 假上游：aipy 返回正常快照
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
