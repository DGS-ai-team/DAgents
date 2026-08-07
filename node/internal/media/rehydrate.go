package media

import (
	"path/filepath"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// ArtifactSSEMap 将 artifact 转为 SSE / hydrate 使用的 media 条目。
func ArtifactSSEMap(art *Artifact) map[string]any {
	if art == nil {
		return nil
	}
	item := map[string]any{
		"id":   art.ID,
		"kind": art.Kind,
		"mime": art.MIME,
		"url":  art.PublicURL(),
	}
	if art.Label != "" {
		item["label"] = art.Label
	}
	if art.Caption != "" {
		item["caption"] = art.Caption
	}
	return item
}

// RehydrateFromMessages 根据历史 tool 消息重建 registry 并返回 tool_call_id → media[]（F-M4）。
func RehydrateFromMessages(reg *Registry, messages []llm.Message, callIndex map[string]llm.ToolCall) map[string][]map[string]any {
	if reg == nil || len(messages) == 0 {
		return nil
	}
	out := make(map[string][]map[string]any)
	for _, msg := range messages {
		if strings.TrimSpace(msg.Role) != "tool" {
			continue
		}
		callID := strings.TrimSpace(msg.ToolCallID)
		if callID == "" {
			continue
		}
		if existing := reg.ArtifactsForToolCall(callID); len(existing) > 0 {
			out[callID] = artifactsToSSE(existing)
			continue
		}
		toolName := strings.TrimSpace(msg.Name)
		var args map[string]any
		if tc, ok := callIndex[callID]; ok {
			if toolName == "" {
				toolName = strings.TrimSpace(tc.Function.Name)
			}
			args = tools.ParseToolArgumentsMap(tc.Function.Arguments)
		}
		specs := tools.ExtractAllToolMediaPaths(toolName, msg.Content, args)
		if len(specs) == 0 {
			continue
		}
		items := make([]map[string]any, 0, len(specs))
		for _, spec := range specs {
			art, err := reg.RegisterFromPath(RegisterOpts{
				RelPath:    spec.RelPath,
				Source:     spec.Source,
				ToolCallID: callID,
				Label:      spec.Label,
				Caption:    spec.Caption,
			})
			if err != nil || art == nil {
				continue
			}
			items = append(items, ArtifactSSEMap(art))
		}
		if len(items) > 0 {
			out[callID] = items
		}
	}
	return out
}

func artifactsToSSE(arts []*Artifact) []map[string]any {
	out := make([]map[string]any, 0, len(arts))
	for _, art := range arts {
		if item := ArtifactSSEMap(art); item != nil {
			out = append(out, item)
		}
	}
	return out
}

// ArtifactsForToolCall 返回已注册 tool call 的 artifact 副本。
func (r *Registry) ArtifactsForToolCall(toolCallID string) []*Artifact {
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Artifact
	for _, art := range r.byID {
		if art == nil || strings.TrimSpace(art.ToolCallID) != id {
			continue
		}
		copy := *art
		copy.RelPath = filepath.ToSlash(copy.RelPath)
		out = append(out, &copy)
	}
	return out
}
