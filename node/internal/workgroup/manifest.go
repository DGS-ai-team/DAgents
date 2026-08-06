package workgroup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	membertools "github.com/DGS-ai-team/DAgents/shared/workgroup"
)

// BuildManifest 从可用工具名构建 §7.1 manifest；revision 由内容哈希派生。
func BuildManifest(nodeID string, toolNames []string, schemas map[string]map[string]any, sideEffects map[string]string) ToolManifest {
	names := append([]string(nil), toolNames...)
	sort.Strings(names)
	catalogSide := membertools.SideEffectClasses()
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
			side = catalogSide[name]
		}
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

// guessSideEffect 仅兜底未知/节点本地工具名；目录内工具以 shared catalog 为准。
func guessSideEffect(name string) string {
	switch {
	case strings.HasPrefix(name, "read_"), name == "glob_files", name == "grep_file", name == "grep_files", name == "show_image":
		return "fs_read"
	case strings.Contains(name, "write") || strings.Contains(name, "edit") || strings.Contains(name, "delete") || name == "search_replace":
		return "fs_write"
	case name == "bash" || name == "bash_run" || strings.HasPrefix(name, "shell") || strings.HasPrefix(name, "background_job_"):
		return "shell"
	default:
		return "other"
	}
}

// WorkspaceToolSchemas 为 workgroup Executor 支持的工具提供 JSON Schema（嵌入目录）。
func WorkspaceToolSchemas() map[string]map[string]any {
	return membertools.ToolSchemas()
}

// WorkspaceSideEffectClasses 工具名 → side_effect（嵌入目录）。
func WorkspaceSideEffectClasses() map[string]string {
	return membertools.SideEffectClasses()
}

// MemberToolCatalogAPI 与 Manage `GET …/meta/member-tools` 同形；供 Node 本地提供（不依赖 Manage）。
func MemberToolCatalogAPI() map[string]any {
	return membertools.APICatalog()
}
