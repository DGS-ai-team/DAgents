package tools

import (
	"context"
	"testing"
)

func TestManagedGoalRejectsTriggerAndChildEscapeTools(t *testing.T) {
	r, err := NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithGoalRun(context.Background(), "goal-1", "run-1")
	for _, name := range []string{"trigger_create", "trigger_update", "create_temporary_agent", "cancel_temporary_agent"} {
		if _, err := r.Execute(ctx, name, `{}`); err == nil {
			t.Fatalf("managed goal unexpectedly executed %s", name)
		}
	}
}
