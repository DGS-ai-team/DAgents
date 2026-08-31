package api

import (
	"encoding/json"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
)

func marshalAgentSnapshot(templateID string, defaults map[string]any, workspace ...agentruntime.WorkspaceConfig) (json.RawMessage, error) {
	if defaults == nil {
		defaults = map[string]any{}
	}
	snap := map[string]any{
		"template_id": templateID,
		"defaults":    defaults,
	}
	if len(workspace) > 0 && strings.TrimSpace(workspace[0].Mode) != "" {
		snap["workspace"] = workspace[0]
	}
	return json.Marshal(snap)
}
