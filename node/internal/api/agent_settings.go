package api

import (
	"encoding/json"
)

func marshalAgentSnapshot(templateID string, defaults map[string]any) (json.RawMessage, error) {
	if defaults == nil {
		defaults = map[string]any{}
	}
	snap := map[string]any{
		"template_id": templateID,
		"defaults":    defaults,
	}
	return json.Marshal(snap)
}
