// Package store 是引擎侧的账号存储抽象。
//
// 与桌面版的区别（重要）：桌面版把 cookie/token 明文写在 accounts/*.json，
// 手机上这是不可接受的做法。这里只定义接口，具体落地交给壳层：
//   - iOS：Swift 侧用 Keychain 存 secret，JSON 里只留 account_id + label + provider
//   - Android：Keystore + EncryptedSharedPreferences
//   - 桌面/CLI（本仓库的 pointsctl）：FileStore，权限 0600，仅供本机调试
//
// 引擎永远拿到的是解密后的 Credential，用完即弃。
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Account 是一条账号记录。Secret 字段在移动端由壳层剥离后单独加密保存。
type Account struct {
	Provider  string `json:"provider"`
	AccountID string `json:"account_id"`
	Label     string `json:"label,omitempty"`
	// SecretJSON 是 provider.Credential 的序列化结果（含 cookie/token）。
	// 在 FileStore 里它明文落盘；在移动端由壳层换成 Keychain 引用。
	SecretJSON json.RawMessage `json:"secret,omitempty"`
	Enabled    bool            `json:"enabled"`
	// Order 是用户拖拽排序后的位置，值越小越靠前。
	// 老数据没有这个字段时一律为 0，此时按存储顺序（即添加顺序）稳定排列，
	// 与升级前的观感一致，不需要数据迁移。
	Order int `json:"order"`
}

// Store 是账号集合的最小接口。
type Store interface {
	List() ([]Account, error)
	Put(a Account) error
	Delete(providerID, accountID string) error
}

// Reorderer 是「用户手动排序」能力的可选扩展。
//
// 与 provider.LotteryProvider 同构：不作为 Store 的必选方法，面板侧用类型断言
// 探测。实现方只需保证 keys 里的账号按数组顺序落库，未列出的账号保持相对次序跟在其后。
type Reorderer interface {
	Reorder(keys []AccountKey) error
}

// AccountKey 是（平台, 账号）二元组，用作排序时的稳定标识。
type AccountKey struct {
	Provider  string `json:"provider"`
	AccountID string `json:"account_id"`
}

// FileStore 是给 CLI 用的文件实现（权限 0600，非加密）。
type FileStore struct {
	mu   sync.Mutex
	path string
}

// NewFileStore 在 dataDir 下创建/打开 accounts.json。
func NewFileStore(dataDir string) (*FileStore, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	return &FileStore{path: filepath.Join(dataDir, "accounts.json")}, nil
}

func (f *FileStore) load() ([]Account, error) {
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Account
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (f *FileStore) save(list []Account) error {
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	// 原子写：先写临时文件再 rename，避免半截 JSON
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

// List 返回全部账号。
func (f *FileStore) List() ([]Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.load()
}

// Put 新增或覆盖同 (provider, account_id) 的记录。
func (f *FileStore) Put(a Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	list, err := f.load()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].Provider == a.Provider && list[i].AccountID == a.AccountID {
			// 覆盖时若调用方没带 Order（0 且原来是别的值），保留原位置，
			// 避免「改个标签/停用账号」把账号莫名其妙挪到第一个。
			if a.Order == 0 && list[i].Order != 0 {
				a.Order = list[i].Order
			}
			list[i] = a
			return f.save(list)
		}
	}
	// 新账号排到最后：已有最大 Order + 1
	maxOrder := -1
	for i := range list {
		if list[i].Order > maxOrder {
			maxOrder = list[i].Order
		}
	}
	a.Order = maxOrder + 1
	return f.save(append(list, a))
}

// Delete 删除一条记录。
func (f *FileStore) Delete(providerID, accountID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	list, err := f.load()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, a := range list {
		if a.Provider == providerID && a.AccountID == accountID {
			continue
		}
		out = append(out, a)
	}
	return f.save(out)
}

// Reorder 按 keys 给出的顺序重排落库。
//
// 语义：keys 里出现的账号按数组下标写入 Order（0,1,2…）；没出现的账号
// （例如刚在另一台设备加的、或 keys 里漏传的）保持它们在原列表中的相对次序，
// 紧跟在已排序账号之后继续编号。这样即使前端只传了一部分也不会丢账号。
func (f *FileStore) Reorder(keys []AccountKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	list, err := f.load()
	if err != nil {
		return err
	}
	type k struct{ p, a string }
	idx := make(map[k]int, len(keys))
	for i, key := range keys {
		idx[k{key.Provider, key.AccountID}] = i
	}
	// 先给命中的账号编号；未命中的先留 -1
	for i := range list {
		if pos, ok := idx[k{list[i].Provider, list[i].AccountID}]; ok {
			list[i].Order = pos
		} else {
			list[i].Order = -1
		}
	}
	// 未命中的接在最后，保持原有相对顺序
	next := len(keys)
	for i := range list {
		if list[i].Order < 0 {
			list[i].Order = next
			next++
		}
	}
	// 落库时按 Order 稳定排序，List 端就不必再排
	sort.SliceStable(list, func(i, j int) bool { return list[i].Order < list[j].Order })
	return f.save(list)
}
