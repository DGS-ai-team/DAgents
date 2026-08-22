package api

import (
	"encoding/json"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
)

func TestSkillMutationResponseUsesStableDiagnosticArrays(t *testing.T) {
	out := skillMutationResponse("agt-1", session.SkillMutationOutcome{
		Action:                      "load_skills",
		Requested:                   []string{"writer"},
		SessionStateAppliedBoundary: "immediate",
		ModelContextAppliedBoundary: "next_human_turn",
		HooksStatus:                 "synchronized",
	})
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"loaded_skills", "rejected", "hooks_loaded", "hooks_failed"} {
		value, ok := decoded[key].([]any)
		if !ok || value == nil {
			t.Fatalf("%s = %#v, want stable empty array", key, decoded[key])
		}
	}
	if decoded["model_context_applied_boundary"] != "next_human_turn" {
		t.Fatalf("boundary = %#v", decoded["model_context_applied_boundary"])
	}
}
