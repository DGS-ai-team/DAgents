package goals

import (
	"testing"
	"time"
)

func validDecision(now time.Time) FinalDecision {
	at := now.Add(2 * time.Hour)
	return FinalDecision{Outcome: OutcomeProgress, Summary: "checking data", Reason: "waiting for data", ExpectedProgress: "import data", NextAction: NextAt, NextWakeAt: &at}
}

func TestValidateFinalDecisionRules(t *testing.T) {
	now := time.Now()
	ctx := DecisionValidationContext{Now: now, MinInterval: time.Minute, ExpiresAt: func() *time.Time { x := now.Add(3 * time.Hour); return &x }(), RegisteredSource: map[string]bool{"source-1": true}}
	if err := ValidateFinalDecision(validDecision(now), ctx); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, code string
		mutate     func(*FinalDecision)
	}{
		{"missing outcome", "invalid_decision_outcome", func(d *FinalDecision) { d.Outcome = "" }},
		{"completed schedules", "completed_must_stop", func(d *FinalDecision) { d.Outcome = OutcomeCompleted }},
		{"completed needs input", "completed_must_stop", func(d *FinalDecision) {
			d.Outcome = OutcomeCompleted
			d.NextAction = NextNeedsInput
			d.NextWakeAt = nil
		}},
		{"past wake", "invalid_decision_next_wake", func(d *FinalDecision) { x := now; d.NextWakeAt = &x }},
		{"at needs expected progress", "invalid_decision_expected_progress", func(d *FinalDecision) { d.Outcome = OutcomeNoChange; d.ExpectedProgress = "" }},
		{"unregistered event", "event_source_not_registered", func(d *FinalDecision) {
			d.NextAction = NextEvent
			d.NextWakeAt = nil
			d.Event = &EventSpec{SourceID: "unknown"}
		}},
		{"command filter", "invalid_decision_event_filter", func(d *FinalDecision) {
			d.NextAction = NextEvent
			d.NextWakeAt = nil
			d.Event = &EventSpec{SourceID: "source-1", Filter: map[string]any{"cmd": "rm"}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validDecision(now)
			tc.mutate(&d)
			if err := ValidateFinalDecision(d, ctx); err == nil || err.Error() != tc.code {
				t.Fatalf("err=%v want %s", err, tc.code)
			}
		})
	}
	missing := FinalDecision{}
	if err := ValidateFinalDecision(missing, ctx); err == nil || err.Error() != "decision_missing" {
		t.Fatalf("missing err=%v", err)
	}
}

func TestFinalDecisionCloneIsDeep(t *testing.T) {
	d := validDecision(time.Now())
	d.Event = &EventSpec{SourceID: "s", Filter: map[string]any{"nested": map[string]any{"items": []any{"keep"}}}}
	c := d.Clone()
	c.Evidence = append(c.Evidence, "changed")
	c.Event.Filter["nested"].(map[string]any)["items"].([]any)[0] = "changed"
	if d.Event.Filter["nested"].(map[string]any)["items"].([]any)[0] != "keep" {
		t.Fatal("clone shared nested filter")
	}
}

func TestDecisionMissingIsStable(t *testing.T) {
	d := FinalDecision{Outcome: OutcomeProgress, Summary: "s", Reason: "x", ExpectedProgress: "y", NextAction: NextNone}
	if err := ValidateFinalDecision(d, DecisionValidationContext{Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
}
