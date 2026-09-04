package api

import (
	"encoding/json"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestOpenAgentMemoryServiceFollowsIndependentCapabilities(t *testing.T) {
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

	tests := []struct {
		name        string
		defaults    map[string]any
		wantService bool
	}{
		{
			name: "automatic recall only",
			defaults: map[string]any{
				"tools":          map[string]any{"enabled_groups": []any{"fs"}},
				"prompt_context": map[string]any{"long_term_enabled": true},
			},
			wantService: true,
		},
		{
			name: "memory tools only",
			defaults: map[string]any{
				"tools":          map[string]any{"enabled_groups": []any{"memory"}},
				"prompt_context": map[string]any{"long_term_enabled": false},
			},
			wantService: true,
		},
		{
			name: "legacy without either capability",
			defaults: map[string]any{
				"tools": map[string]any{"enabled_groups": []any{"fs"}},
			},
			wantService: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, err := srv.openAgentMemoryService("agt-memory-capability", newRecord(tc.defaults))
			if err != nil {
				t.Fatal(err)
			}
			if (service != nil) != tc.wantService {
				t.Fatalf("service present=%v want %v", service != nil, tc.wantService)
			}
			if service != nil {
				t.Cleanup(func() { _ = service.Close() })
			}
		})
	}
}
