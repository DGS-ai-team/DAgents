package agentruntime

import "testing"

func TestMergeDefaults_nestedTools(t *testing.T) {
	base := map[string]any{
		"tools": map[string]any{
			"enabled_groups": []any{"fs", "skills"},
		},
		"llm": map[string]any{
			"max_steps": float64(32),
		},
	}
	override := map[string]any{
		"tools": map[string]any{
			"enabled_groups": []any{"fs", "bash"},
		},
	}
	out := MergeDefaults(base, override)
	tools, ok := out["tools"].(map[string]any)
	if !ok {
		t.Fatalf("tools = %#v", out["tools"])
	}
	groups, ok := tools["enabled_groups"].([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("enabled_groups = %#v", tools["enabled_groups"])
	}
	llm, ok := out["llm"].(map[string]any)
	if !ok || llm["max_steps"] != float64(32) {
		t.Fatalf("llm = %#v", out["llm"])
	}
}
