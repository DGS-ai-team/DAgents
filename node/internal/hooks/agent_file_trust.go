package hooks

import (
	"strings"
	"sync"
	"time"
)

// AgentFileTrust 为 session 级 Agent 自有文件写操作信任表（per-session，不持久化）。
type AgentFileTrust struct {
	mu      sync.Mutex
	entries map[string]agentFileTrustEntry
}

type agentFileTrustEntry struct {
	Owned          bool
	LastWriteMtime time.Time
	PendingCreate  bool
}

// NewAgentFileTrust 构造空信任表。
func NewAgentFileTrust() *AgentFileTrust {
	return &AgentFileTrust{entries: make(map[string]agentFileTrustEntry)}
}

// NormalizePathKey 规范化 FS 相对路径键（与 tools.cachePathKey 一致）。
func NormalizePathKey(relPath string) string {
	return strings.TrimSpace(strings.ReplaceAll(relPath, "\\", "/"))
}

// IsOwned 判断 path 是否已标记为 Agent 创建且处于信任链。
func (t *AgentFileTrust) IsOwned(pathKey string) bool {
	if t == nil {
		return false
	}
	pathKey = NormalizePathKey(pathKey)
	if pathKey == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ent, ok := t.entries[pathKey]
	return ok && ent.Owned
}

// LastWriteMtime 返回信任表记录的上次 Agent 写后 mtime。
func (t *AgentFileTrust) LastWriteMtime(pathKey string) (time.Time, bool) {
	if t == nil {
		return time.Time{}, false
	}
	pathKey = NormalizePathKey(pathKey)
	t.mu.Lock()
	defer t.mu.Unlock()
	ent, ok := t.entries[pathKey]
	if !ok || !ent.Owned {
		return time.Time{}, false
	}
	return ent.LastWriteMtime, true
}

// SetPendingCreate 在 write_file 首次创建（before_each ENOENT）时标记待落盘。
func (t *AgentFileTrust) SetPendingCreate(pathKey string) {
	if t == nil {
		return
	}
	pathKey = NormalizePathKey(pathKey)
	if pathKey == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ent := t.entries[pathKey]
	ent.PendingCreate = true
	t.entries[pathKey] = ent
}

// MarkOwned 在 write_file 创建成功后标记 agentOwned 并记录 mtime。
func (t *AgentFileTrust) MarkOwned(pathKey string, mtime time.Time) {
	if t == nil {
		return
	}
	pathKey = NormalizePathKey(pathKey)
	if pathKey == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[pathKey] = agentFileTrustEntry{
		Owned:          true,
		LastWriteMtime: mtime,
	}
}

// UpdateMtime 在信任 path 写成功后刷新 mtime。
func (t *AgentFileTrust) UpdateMtime(pathKey string, mtime time.Time) {
	if t == nil {
		return
	}
	pathKey = NormalizePathKey(pathKey)
	if pathKey == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ent, ok := t.entries[pathKey]
	if !ok || !ent.Owned {
		return
	}
	ent.LastWriteMtime = mtime
	ent.PendingCreate = false
	t.entries[pathKey] = ent
}

// ConsumePendingCreate 读取并清除 pendingCreate（write_file 创建成功路径）。
func (t *AgentFileTrust) ConsumePendingCreate(pathKey string) bool {
	if t == nil {
		return false
	}
	pathKey = NormalizePathKey(pathKey)
	t.mu.Lock()
	defer t.mu.Unlock()
	ent, ok := t.entries[pathKey]
	if !ok || !ent.PendingCreate {
		return false
	}
	ent.PendingCreate = false
	t.entries[pathKey] = ent
	return true
}

// WriteToolRelPath 从 write_file / search_replace 参数提取 path。
func WriteToolRelPath(toolName string, args map[string]any) string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "write_file", "search_replace":
		if p, ok := args["path"].(string); ok {
			return NormalizePathKey(p)
		}
	}
	return ""
}
