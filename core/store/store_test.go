package store

import "testing"

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return fs
}

func ids(list []Account) []string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.AccountID
	}
	return out
}

func TestPutAppendsInOrder(t *testing.T) {
	fs := newTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := fs.Put(Account{Provider: "p", AccountID: id, Enabled: true}); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}
	list, _ := fs.List()
	got := ids(list)
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序 = %v, 期望 %v", got, want)
		}
	}
	if list[2].Order != 2 {
		t.Fatalf("c 的 Order = %d, 期望 2", list[2].Order)
	}
}

func TestReorder(t *testing.T) {
	fs := newTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		_ = fs.Put(Account{Provider: "p", AccountID: id, Enabled: true})
	}
	err := fs.Reorder([]AccountKey{
		{Provider: "p", AccountID: "c"},
		{Provider: "p", AccountID: "a"},
		{Provider: "p", AccountID: "b"},
	})
	if err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	list, _ := fs.List()
	got := ids(list)
	want := []string{"c", "a", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("重排后 = %v, 期望 %v", got, want)
		}
	}
	// Order 应重新编号为 0,1,2
	for i, a := range list {
		if a.Order != i {
			t.Fatalf("%s 的 Order = %d, 期望 %d", a.AccountID, a.Order, i)
		}
	}
}

// 只传一部分 key 时，未提到的账号不能被丢掉，且要排在已排序账号之后。
func TestReorderPartialKeepsOthers(t *testing.T) {
	fs := newTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		_ = fs.Put(Account{Provider: "p", AccountID: id, Enabled: true})
	}
	if err := fs.Reorder([]AccountKey{{Provider: "p", AccountID: "c"}}); err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	list, _ := fs.List()
	if len(list) != 3 {
		t.Fatalf("账号数 = %d, 期望 3", len(list))
	}
	if list[0].AccountID != "c" {
		t.Fatalf("首位 = %s, 期望 c", list[0].AccountID)
	}
	// 其余保持原有相对次序 a, b
	if list[1].AccountID != "a" || list[2].AccountID != "b" {
		t.Fatalf("尾部顺序 = %v, 期望 [a b]", ids(list)[1:])
	}
}

// 停用/改标签走 Put 覆盖时，位置必须保持不变。
func TestPutKeepsOrderOnUpdate(t *testing.T) {
	fs := newTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		_ = fs.Put(Account{Provider: "p", AccountID: id, Enabled: true})
	}
	// 把 c 提到最前
	_ = fs.Reorder([]AccountKey{
		{Provider: "p", AccountID: "c"},
		{Provider: "p", AccountID: "a"},
		{Provider: "p", AccountID: "b"},
	})
	// 模拟 toggle：从 List 取原记录再写回
	list, _ := fs.List()
	target := list[2] // b
	target.Enabled = false
	if err := fs.Put(target); err != nil {
		t.Fatalf("Put: %v", err)
	}
	after, _ := fs.List()
	got := ids(after)
	want := []string{"c", "a", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("更新后顺序 = %v, 期望 %v", got, want)
		}
	}
	if after[2].Enabled {
		t.Fatal("b 应为停用")
	}
}
