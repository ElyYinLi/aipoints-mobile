package provider

import (
	"sort"
	"sync"

	"github.com/ElyYinLi/aipoints-mobile/core/httpx"
)

// Registry 是平台注册表。新增平台只需在这里登记一行，壳层无需改动。
type Registry struct {
	mu sync.RWMutex
	m  map[string]Provider
}

// NewRegistry 构造默认注册表（当前先接入三家可验证的平台）。
//
// 未接入的原因写在 spec/api-map.json 的 mobile 字段里：
//   - trae      httpOnly cookie → 需要 WebView 取 JWT，壳层就绪后再加
//   - minimax   签名头盐值未确认（需真实 token 反推）
//   - doubao    只能页面自动化，需要 WebView 驱动
//   - qoder     桌面端指纹 runtime-info.exe，移动端无解
func NewRegistry(c *httpx.Client) *Registry {
	r := &Registry{m: make(map[string]Provider)}
	for _, p := range []Provider{
		NewWorkBuddy(c),
		NewLingxi(c),
		NewDumate(c),
	} {
		r.m[p.Meta().ID] = p
	}
	return r
}

// Get 按 id 取适配器。
func (r *Registry) Get(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[id]
	return p, ok
}

// Register 运行时注册（壳层做灰度或插件化时用）。
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[p.Meta().ID] = p
}

// All 返回按 id 排序的元信息，UI 直接渲染。
func (r *Registry) All() []Meta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Meta, 0, len(r.m))
	for _, p := range r.m {
		out = append(out, p.Meta())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
