package triggers

import (
	"context"
	"testing"
	"time"
)

type managedSubmitter struct{ fired int }

func (m *managedSubmitter) EnsureSession(string) (string, error)              { return "s", nil }
func (m *managedSubmitter) SubmitTriggerMessage(string, string, string) error { m.fired++; return nil }

func TestManagedSchedulerRunOnceUsesHookAndSkipsUntilNextWake(t *testing.T) {
	st, _ := OpenStore(t.TempDir()+"/triggers.json", 20)
	now := time.Now()
	next := now.Add(-time.Second)
	d := Definition{TriggerID: "managed", Name: "g", Condition: map[string]any{"interval_seconds": 60}, TargetAgentID: "a", TaskTemplate: "x", Enabled: true, ManagedGoalID: "goal", NextFireAt: func() *float64 { v := float64(next.UnixNano()) / 1e9; return &v }()}
	if _, e := st.CreateTrigger(d); e != nil {
		t.Fatal(e)
	}
	sub := &managedSubmitter{}
	sch := NewScheduler(st, sub, 60)
	calls := 0
	sch.SetManagedFire(func(_ context.Context, _ Definition, _ time.Time) FireRecord {
		calls++
		return FireRecord{Status: FireStatusQueued}
	})
	sch.RunOnceForTest(context.Background(), now)
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}
