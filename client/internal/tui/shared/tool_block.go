package shared

import (
	"fmt"
	"sync"
)

// ToolBlockRegistry 跟踪 transcript 中可展开 tool 块状态（display 层，不改存储协议）。
type ToolBlockRegistry struct {
	mu       sync.Mutex
	expanded map[string]bool
	order    []string
	seq      int
}

// NewToolBlockRegistry 创建空 registry。
func NewToolBlockRegistry() *ToolBlockRegistry {
	return &ToolBlockRegistry{
		expanded: make(map[string]bool),
	}
}

// Reset 清空（切换 session 时）。
func (r *ToolBlockRegistry) Reset() {
	r.mu.Lock()
	r.expanded = make(map[string]bool)
	r.order = nil
	r.seq = 0
	r.mu.Unlock()
}

// NextSeqID 生成顺序 tool 块 ID（SSE 无 call_id 时）。
func (r *ToolBlockRegistry) NextSeqID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return fmt.Sprintf("tool-seq-%d", r.seq)
}

// Register 登记 tool 块 ID（tool_result 到达时）。
func (r *ToolBlockRegistry) Register(id string) {
	id = trimDisplayField(id)
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.expanded[id]; !ok {
		r.order = append(r.order, id)
	}
}

// Expand 展开指定块。
func (r *ToolBlockRegistry) Expand(id string) {
	r.setExpanded(id, true)
}

// Collapse 收起指定块。
func (r *ToolBlockRegistry) Collapse(id string) {
	r.setExpanded(id, false)
}

func (r *ToolBlockRegistry) setExpanded(id string, on bool) {
	id = trimDisplayField(id)
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expanded[id] = on
}

// IsExpanded 是否展开；verbose 全局为 true 时视为全部展开。
func (r *ToolBlockRegistry) IsExpanded(id string, verbose bool) bool {
	if verbose {
		return true
	}
	id = trimDisplayField(id)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.expanded[id]
}

// LastID 返回最近登记的 tool 块 ID。
func (r *ToolBlockRegistry) LastID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.order) == 0 {
		return ""
	}
	return r.order[len(r.order)-1]
}

// ExpandLast 展开最近 tool 块。
func (r *ToolBlockRegistry) ExpandLast() string {
	id := r.LastID()
	if id == "" {
		return ""
	}
	r.Expand(id)
	return id
}

// CollapseLast 收起最近 tool 块。
func (r *ToolBlockRegistry) CollapseLast() string {
	id := r.LastID()
	if id == "" {
		return ""
	}
	r.Collapse(id)
	return id
}
