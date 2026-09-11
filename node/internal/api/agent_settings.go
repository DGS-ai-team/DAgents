package api

import (
	"encoding/json"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
)

func marshalAgentSnapshot(templateID string, defaults map[string]any, workspace ...agentruntime.WorkspaceConfig) (json.RawMessage, error) {
	return marshalAgentSnapshotWithType("normal", templateID, defaults, workspace...)
}

func marshalAgentSnapshotWithType(agentType, templateID string, defaults map[string]any, workspace ...agentruntime.WorkspaceConfig) (json.RawMessage, error) {
	if defaults == nil {
		defaults = map[string]any{}
	}
	snap := map[string]any{
		"agent_type":  normalizeAgentType(agentType),
		"template_id": templateID,
		"defaults":    defaults,
	}
	if len(workspace) > 0 && strings.TrimSpace(workspace[0].Mode) != "" {
		snap["workspace"] = workspace[0]
	}
	return json.Marshal(snap)
}

func normalizeAgentType(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), "auto") {
		return "auto"
	}
	return "normal"
}
