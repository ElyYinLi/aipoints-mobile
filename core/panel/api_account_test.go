package panel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ElyYinLi/aipoints-mobile/core/store"
)

// memStore 是可读写的内存账号库，用来测「添加 → 导出 → 删除 → 导入」全链路。
type memStore struct {
	mu   sync.Mutex
	list []store.Account
}

func (m *memStore) List() ([]store.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.Account, len(m.list))
	copy(out, m.list)
	return out, nil
}

func (m *memStore) Put(a store.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.list {
		if m.list[i].Provider == a.Provider && m.list[i].AccountID == a.AccountID {
			m.list[i] = a
			return nil
		}
	}
	m.list = append(m.list, a)
	return nil
}

func (m *memStore) Delete(p, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.list[:0]
	for _, a := range m.list {
		if a.Provider == p && a.AccountID == id {
			continue
		}
		out = append(out, a)
	}
	m.list = out
	return nil
}

func newAPIServer(t *testing.T) (*httptest.Server, *memStore) {
	t.Helper()
	ms := &memStore{}
	s := New(Options{Store: ms, Version: "test-0.3.1"})
	// 直接用 New 构造好的 Handler，覆盖真实路由表（含 loopbackOnly 中间件）
	srv := httptest.NewServer(s.srv.Handler)
	t.Cleanup(srv.Close)
	return srv, ms
}

func postJSON(t *testing.T, url string, body any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("响应不是 JSON: %v", err)
	}
	return m
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("响应不是 JSON: %v", err)
	}
	return m
}

// TestAccountAddRejectsInvalid 空凭据 / 未知平台必须拒绝，不能写进账号库。
func TestAccountAddRejectsInvalid(t *testing.T) {
	srv, ms := newAPIServer(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"空凭据", map[string]any{"provider": "aipy"}},
		{"未知平台", map[string]any{"provider": "not-a-platform", "cookie": "a=1"}},
		{"缺平台", map[string]any{"cookie": "a=1"}},
	}
	for _, c := range cases {
		m := postJSON(t, srv.URL+"/api/account/add", c.body)
		if m["ok"] != false {
			t.Errorf("%s 应被拒绝，实际 %v", c.name, m)
		}
	}
	list, _ := ms.List()
	if len(list) != 0 {
		t.Fatalf("非法请求不应写入账号库，实际 %d 条", len(list))
	}
}

// TestAccountLifecycle 用户反馈的三条主流程：手动添加、删除、导出/导入恢复。
func TestAccountLifecycle(t *testing.T) {
	srv, ms := newAPIServer(t)

	// 1) 手动添加
	add := postJSON(t, srv.URL+"/api/account/add", map[string]any{
		"provider": "aipy", "label": "主号", "cookie": "session=abc123; uid=42",
	})
	if add["ok"] != true {
		t.Fatalf("添加失败: %v", add)
	}
	id, _ := add["account_id"].(string)
	if id == "" {
		t.Fatal("添加成功应返回 account_id")
	}
	list, _ := ms.List()
	if len(list) != 1 || list[0].Provider != "aipy" || list[0].Label != "主号" {
		t.Fatalf("账号库内容不对: %+v", list)
	}
	// secret 必须真的存进去，不能只存脱敏字段
	var sec map[string]any
	_ = json.Unmarshal(list[0].SecretJSON, &sec)
	if sec["cookie"] != "session=abc123; uid=42" {
		t.Errorf("凭据未写入: %v", sec)
	}

	// 2) 同一份凭据重复添加应覆盖而非新增（account_id 稳定）
	postJSON(t, srv.URL+"/api/account/add", map[string]any{
		"provider": "aipy", "label": "改名了", "cookie": "session=abc123; uid=42",
	})
	list, _ = ms.List()
	if len(list) != 1 || list[0].Label != "改名了" {
		t.Fatalf("重复添加应覆盖，实际 %+v", list)
	}

	// 3) 停用 / 启用
	tg := postJSON(t, srv.URL+"/api/account/toggle", map[string]any{
		"provider": "aipy", "account_id": id, "enabled": false,
	})
	if tg["ok"] != true || tg["enabled"] != false {
		t.Fatalf("停用失败: %v", tg)
	}
	list, _ = ms.List()
	if list[0].Enabled {
		t.Fatal("停用后 Enabled 应为 false")
	}

	// 4) 导出：必须含明文凭据，否则备份没有意义
	exp := getJSON(t, srv.URL+"/api/export")
	if exp["version"] != float64(1) {
		t.Fatalf("导出格式不对: %v", exp)
	}
	accts, _ := exp["accounts"].([]any)
	if len(accts) != 1 {
		t.Fatalf("导出应有 1 条账号，实际 %d", len(accts))
	}
	first, _ := accts[0].(map[string]any)
	secret, _ := first["secret"].(map[string]any)
	if secret["cookie"] != "session=abc123; uid=42" {
		t.Fatalf("导出丢了凭据: %v", first)
	}
	backup, _ := json.Marshal(exp)

	// 5) 删除
	del := postJSON(t, srv.URL+"/api/account/delete", map[string]any{
		"provider": "aipy", "account_id": id,
	})
	if del["ok"] != true {
		t.Fatalf("删除失败: %v", del)
	}
	list, _ = ms.List()
	if len(list) != 0 {
		t.Fatalf("删除后应为空，实际 %d 条", len(list))
	}

	// 6) 从备份恢复（支持 {"text":"..."} 直接粘贴）
	imp := postJSON(t, srv.URL+"/api/import", map[string]any{"text": string(backup)})
	if imp["ok"] != true || imp["added"] != float64(1) {
		t.Fatalf("导入失败: %v", imp)
	}
	list, _ = ms.List()
	if len(list) != 1 || list[0].AccountID != id {
		t.Fatalf("导入未恢复原账号: %+v", list)
	}
	var sec2 map[string]any
	_ = json.Unmarshal(list[0].SecretJSON, &sec2)
	if sec2["cookie"] != "session=abc123; uid=42" {
		t.Fatalf("导入后凭据丢失: %v", sec2)
	}
}

// TestImportRejectsGarbage 备份文本乱填时不能把账号库搞坏。
func TestImportRejectsGarbage(t *testing.T) {
	srv, ms := newAPIServer(t)
	for _, body := range []any{
		map[string]any{"text": "not json"},
		map[string]any{"text": `{"version":1,"accounts":[]}`},
		map[string]any{"text": `{"accounts":[{"provider":"aipy","account_id":"x","secret":"not-object"}]}`},
	} {
		m := postJSON(t, srv.URL+"/api/import", body)
		if m["ok"] != false {
			t.Errorf("非法备份应被拒绝: %v -> %v", body, m)
		}
	}
	list, _ := ms.List()
	if len(list) != 0 {
		t.Fatalf("非法导入不应写入，实际 %d 条", len(list))
	}
}

// TestProbeRejectsMissingCredential 试连不能空凭据打上游。
func TestProbeRejectsMissingCredential(t *testing.T) {
	srv, _ := newAPIServer(t)
	m := postJSON(t, srv.URL+"/api/probe", map[string]any{"provider": "aipy"})
	if m["ok"] != false || !strings.Contains(m["error"].(string), "Cookie") {
		t.Fatalf("应提示填凭据，实际 %v", m)
	}
}

// TestStableAccountID 主键必须跨调用稳定，否则重启后 cookie 读不回来。
func TestStableAccountID(t *testing.T) {
	a := stableAccountID("aipy", "session=abc")
	b := stableAccountID("aipy", "session=abc")
	c := stableAccountID("aipy", "session=def")
	if a != b || a == c || len(a) != 16 {
		t.Fatalf("主键不稳定或长度不对: %s %s %s", a, b, c)
	}
}
