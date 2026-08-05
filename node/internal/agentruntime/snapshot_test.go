package agentruntime

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestEffectiveFSRoot_alwaysNodeRoot(t *testing.T) {
	snap := Snapshot{}
	got := EffectiveFSRoot("/rt", "agt-1", snap)
	if got != "/rt" {
		t.Fatalf("got %q want /rt", got)
	}
}

func TestEnabledToolGroups(t *testing.T) {
	snap := Snapshot{Defaults: map[string]any{
		"tools": map[string]any{"enabled_groups": []any{"fs", "skills"}},
	}}
	got := EnabledToolGroups(snap)
	if len(got) != 2 || got[0] != "fs" {
		t.Fatalf("got %v", got)
	}
}

func TestParseSnapshot_ignoresLegacySandbox(t *testing.T) {
	raw := []byte(`{"template_id":"x","sandbox":{"enabled":true,"fs_root_isolation":true},"defaults":{"tools":{"enabled_groups":["fs"]}}}`)
	snap, err := ParseSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TemplateID != "x" {
		t.Fatalf("template_id=%q", snap.TemplateID)
	}
	if EffectiveFSRoot("/rt", "agt-1", snap) != "/rt" {
		t.Fatal("legacy sandbox must not change FS root")
	}
	groups := EnabledToolGroups(snap)
	if len(groups) != 1 || groups[0] != "fs" {
		t.Fatalf("groups=%v", groups)
	}
}

func TestEffectiveMultimodalEnabled_fromNodeProfile(t *testing.T) {
	mm := true
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	cfg.LLM.Profiles = map[string]config.LLMProfileConfig{
		"text": {Provider: "deepseek", Model: "deepseek-chat"},
		"vision": {
			Provider:          "openai",
			Model:             "gpt-4o",
			MultimodalEnabled: &mm,
		},
	}
	cfg.LLM.Active = "text"
	cfg.ApplyDefaults()

	snap := Snapshot{Defaults: map[string]any{
		"llm": map[string]any{"active": "vision"},
	}}
	if !EffectiveMultimodalEnabled(cfg, snap) {
		t.Fatal("expected multimodal from node profile vision")
	}
	snapText := Snapshot{Defaults: map[string]any{
		"llm": map[string]any{"active": "text"},
	}}
	if EffectiveMultimodalEnabled(cfg, snapText) {
		t.Fatal("text profile should not enable multimodal")
	}
}

func TestApplyDefaultsToTurnOptions_maxToolLoopsFromSnapshot(t *testing.T) {
	var turn session.TurnOptions
	ApplyDefaultsToTurnOptions(&turn, Snapshot{Defaults: map[string]any{
		"llm": map[string]any{"max_tool_loops": float64(8)},
	}})
	if turn.MaxToolLoops != 8 {
		t.Fatalf("got %d want 8", turn.MaxToolLoops)
	}
	var empty session.TurnOptions
	empty.MaxToolLoops = 99 // BaseTurn 值不得覆盖 Agent 缺省
	ApplyDefaultsToTurnOptions(&empty, Snapshot{})
	if empty.MaxToolLoops != DefaultMaxToolLoops {
		t.Fatalf("got %d want creation default %d", empty.MaxToolLoops, DefaultMaxToolLoops)
	}
}
