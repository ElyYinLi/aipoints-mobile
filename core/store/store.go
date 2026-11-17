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
}

// Store 是账号集合的最小接口。
type Store interface {
	List() ([]Account, error)
	Put(a Account) error
	Delete(providerID, accountID string) error
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
			list[i] = a
			return f.save(list)
		}
	}
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
