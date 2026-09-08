package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func hasAutonomyDef(defs []ToolDef) bool {
	for _, d := range defs {
		if strings.HasPrefix(d.Function.Name, "autonomy_") {
			return true
		}
	}
	return false
}

func TestAutonomyToolsAreExplicitlyIsolatedAndBound(t *testing.T) {
	r, err := NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if hasAutonomyDef(r.Definitions()) {
		t.Fatal("normal registry exposed autonomy tools")
	}
	var gotID string
	r.SetAutonomyRuntime(true, func(_ context.Context, id string) (any, error) {
		gotID = id
		return map[string]any{"status": "disabled"}, nil
	}, func(_ context.Context, id string, _ AutonomyUpdate) (any, error) {
		gotID = id
		return map[string]any{"status": "paused"}, nil
	})
	r.SetAgentID("bound-agent")
	if !hasAutonomyDef(r.Definitions()) {
		t.Fatal("auto registry omitted autonomy tools")
	}
	if _, err := r.Execute(context.Background(), "autonomy_get", `{}`); err != nil {
		t.Fatal(err)
	}
	if gotID != "bound-agent" {
		t.Fatalf("callback agent id=%q", gotID)
	}
	if _, err := r.Execute(context.Background(), "autonomy_update", `{"agent_id":"other"}`); err == nil {
		t.Fatal("accepted model supplied agent id")
	}
	goalCtx := WithGoalRun(context.Background(), "goal", "run")
	if _, err := r.Execute(goalCtx, "autonomy_get", `{}`); err == nil {
		t.Fatal("dedicated goal context exposed autonomy")
	}
}

func TestAutonomyUpdateRejectsBudgetAndInvalidFields(t *testing.T) {
	r, err := NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	r.SetAutonomyRuntime(true, nil, func(_ context.Context, _ string, in AutonomyUpdate) (any, error) { return json.Marshal(in) })
	for _, raw := range []string{`{"token_budget":999}`, `{"max_runs":99}`, `{}`, `{"objective":" "}`} {
		if _, err := r.Execute(context.Background(), "autonomy_update", raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
