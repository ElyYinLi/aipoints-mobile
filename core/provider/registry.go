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

// NewRegistry 构造默认注册表（已接入五家）。
//
// 未接入/放弃的原因写在 spec/api-map.json 的 mobile 字段里：
//   - minimax   签名头盐值未确认（需真实 token 反推），暂时放弃
//   - doubao    只能页面自动化，需要 WebView 驱动，收益低于成本，放弃
//   - qoder     领取依赖桌面端 runtime-info.exe 指纹，移动端无解，放弃
func NewRegistry(c *httpx.Client) *Registry {
	r := &Registry{m: make(map[string]Provider)}
	for _, p := range []Provider{
		NewWorkBuddy(c),
		NewAiPy(c),
		NewDumate(c),
		NewLingxi(c),
		NewTrae(c),
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
