package api

import (
	"encoding/json"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestOpenAgentMemoryServiceAlwaysUsesWorkspaceStore(t *testing.T) {
	cfg := &config.Config{RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	srv := &Server{cfg: cfg}

	newRecord := func(defaults map[string]any) *store.AgentRecord {
		raw, err := json.Marshal(map[string]any{"defaults": defaults})
		if err != nil {
			t.Fatal(err)
		}
		return &store.AgentRecord{AgentID: "agt-memory-capability", ConfigSnapshot: raw}
	}

	for _, defaults := range []map[string]any{
		{"tools": map[string]any{"enabled_groups": []any{"fs"}}, "prompt_context": map[string]any{"memory_enabled": true}},
		{"tools": map[string]any{"enabled_groups": []any{"memory"}}, "prompt_context": map[string]any{"memory_enabled": false}},
		{"tools": map[string]any{"enabled_groups": []any{"fs"}}},
	} {
		service, err := srv.openAgentMemoryService("agt-memory-capability", newRecord(defaults))
		if err != nil {
			t.Fatal(err)
		}
		if service == nil {
			t.Fatal("memory service must be present for every Agent runtime")
		}
		_ = service.Close()
	}
}
