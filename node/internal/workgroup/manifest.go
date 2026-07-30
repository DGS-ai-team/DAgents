package workgroup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// BuildManifest 从可用工具名构建 §7.1 manifest；revision 由内容哈希派生。
func BuildManifest(nodeID string, toolNames []string, schemas map[string]map[string]any, sideEffects map[string]string) ToolManifest {
	names := append([]string(nil), toolNames...)
	sort.Strings(names)
	tools := make([]ToolCatalogEntry, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		schema := schemas[name]
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		side := sideEffects[name]
		if side == "" {
			side = guessSideEffect(name)
		}
		tools = append(tools, ToolCatalogEntry{
			Name:            name,
			JSONSchema:      schema,
			SideEffectClass: side,
			ExecutionMode:   "sync",
		})
	}
	rev := catalogRevision(nodeID, tools)
	return ToolManifest{
		NodeID:              nodeID,
		ToolCatalogRevision: rev,
		Tools:               tools,
	}
}

func catalogRevision(nodeID string, tools []ToolCatalogEntry) string {
	payload := map[string]any{
		"node_id": nodeID,
		"tools":   tools,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "rev_unknown"
	}
	sum := sha256.Sum256(raw)
	return "rev_" + hex.EncodeToString(sum[:8])
}

func guessSideEffect(name string) string {
	switch {
	case strings.HasPrefix(name, "read_"):
		return "fs_read"
	case strings.Contains(name, "write") || strings.Contains(name, "edit") || strings.Contains(name, "delete"):
		return "fs_write"
	case name == "bash" || strings.HasPrefix(name, "shell"):
		return "shell"
	default:
		return "other"
	}
}

// FilterAvailable 从 Node 全量工具中按 allow 交集过滤。
func FilterAvailable(nodeAll []string, allow []string) []string {
	return EffectiveToolNames(allow, allow, nodeAll)
}
