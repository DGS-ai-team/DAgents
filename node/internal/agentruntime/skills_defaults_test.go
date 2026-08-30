package agentruntime

import "testing"

func TestSkillsFromDefaults(t *testing.T) {
	t.Run("missing skills block unrestricted", func(t *testing.T) {
		got := SkillsFromDefaults(Snapshot{})
		if got.VisibleRestrict {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("enabled false ignored", func(t *testing.T) {
		got := SkillsFromDefaults(Snapshot{Defaults: map[string]any{
			"skills": map[string]any{"enabled": false},
		}})
		if got.VisibleRestrict {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("visible empty restricts to none", func(t *testing.T) {
		got := SkillsFromDefaults(Snapshot{Defaults: map[string]any{
			"skills": map[string]any{"enabled": true, "visible": []any{}},
		}})
		if !got.VisibleRestrict || len(got.Visible) != 0 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("visible list", func(t *testing.T) {
		got := SkillsFromDefaults(Snapshot{Defaults: map[string]any{
			"skills": map[string]any{"enabled": true, "visible": []any{"a", " b ", "a", ""}},
		}})
		if !got.VisibleRestrict || len(got.Visible) != 2 || got.Visible[0] != "a" || got.Visible[1] != "b" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("catalog tool mode is no longer a setting", func(t *testing.T) {
		got := SkillsFromDefaults(Snapshot{Defaults: map[string]any{
			"skills": map[string]any{"catalog_tool_mode": true},
		}})
		if got.VisibleRestrict {
			t.Fatalf("obsolete catalog setting changed skills visibility: %+v", got)
		}
	})
}
