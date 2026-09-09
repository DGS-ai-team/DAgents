package triggers

import (
	"context"
	"testing"
	"time"
)

type retiredTestSubmitter struct{ calls int }

func (s *retiredTestSubmitter) EnsureSession(string) (string, error) { return "session", nil }
func (s *retiredTestSubmitter) SubmitTriggerMessage(string, string, string) error {
	s.calls++
	return nil
}

func TestRetiredControllerIsDisabledAndCannotBeScheduled(t *testing.T) {
	st, err := OpenStore(t.TempDir()+"/triggers.json", 20)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateTrigger(Definition{
		TriggerID: "legacy-managed", OwnerAgentID: "a", TargetAgentID: "a",
		Controller: "goal", ControllerID: "legacy-goal", ManagedGoalID: "legacy-goal",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ValidateOwners(map[string]bool{"a": true}); err != nil {
		t.Fatal(err)
	}
	d, ok := st.GetTrigger("legacy-managed")
	if !ok {
		t.Fatal("legacy trigger disappeared")
	}
	if d.Enabled || !d.RecoveryRequired {
		t.Fatalf("legacy controller remained executable: %+v", d)
	}
}

func TestRetiredControllerCannotFireBeforeOwnerValidation(t *testing.T) {
	st, err := OpenStore(t.TempDir()+"/triggers.json", 20)
	if err != nil {
		t.Fatal(err)
	}
	next := float64(time.Now().Add(-time.Second).UnixNano()) / 1e9
	_, err = st.CreateTrigger(Definition{
		TriggerID: "legacy-fire", OwnerAgentID: "a", TargetAgentID: "a",
		Controller: "goal", ControllerID: "g", ManagedGoalID: "g", Enabled: true,
		Condition: map[string]any{"interval_seconds": 60}, NextFireAt: &next,
	})
	if err != nil {
		t.Fatal(err)
	}
	sub := &retiredTestSubmitter{}
	sch := NewScheduler(st, sub, 60)
	if _, err := sch.FireTrigger("legacy-fire", "manual", nil, true, nil); err != nil {
		t.Fatal(err)
	}
	sch.RunOnceForTest(context.Background(), time.Now())
	if sub.calls != 0 {
		t.Fatalf("retired controller was delivered: calls=%d", sub.calls)
	}
}
