package goals

import (
	"encoding/json"
	"math"
	"strings"
	"time"
)

type Outcome string

const (
	OutcomeProgress  Outcome = "progress"
	OutcomeNoChange  Outcome = "no_change"
	OutcomeCompleted Outcome = "completed"
	OutcomeBlocked   Outcome = "blocked"
)

type NextAction string

const (
	NextAt         NextAction = "at"
	NextEvent      NextAction = "event"
	NextNone       NextAction = "none"
	NextNeedsInput NextAction = "needs_input"
)

// FinalDecision is the model's structured, terminal decision for one turn.
// It is data only: validation never schedules or wakes a goal.
type FinalDecision struct {
	Outcome              Outcome    `json:"outcome"`
	Summary              string     `json:"summary,omitempty"`
	Evidence             []string   `json:"evidence,omitempty"`
	NextAction           NextAction `json:"next_action"`
	NextWakeAt           *time.Time `json:"next_wake_at,omitempty"`
	NextWakeAfterSeconds *int64     `json:"next_wake_after_seconds,omitempty"`
	Event                *EventSpec `json:"event,omitempty"`
	Reason               string     `json:"reason"`
	ExpectedProgress     string     `json:"expected_progress,omitempty"`
}

// EventSpec refers to a registered event source and a deliberately limited
// data filter. It cannot contain executable command fields.
type EventSpec struct {
	SourceID string `json:"source_id"`
	// Filter has one deliberately fixed form: {"equals":{"field":"value"}}.
	// It is data matching only and has no operators or executable expressions.
	Filter map[string]any `json:"filter,omitempty"`
}

type DecisionValidationContext struct {
	Now              time.Time
	MinInterval      time.Duration
	ExpiresAt        *time.Time
	RegisteredSource map[string]bool
}

type DecisionError struct{ Code string }

func (e *DecisionError) Error() string { return e.Code }

func decisionErr(code string) error { return &DecisionError{Code: code} }

// ValidateFinalDecision validates a decision against the current goal policy.
// The returned error codes are stable and suitable for API responses.
func ValidateFinalDecision(d FinalDecision, ctx DecisionValidationContext) error {
	if d.Outcome == "" && d.NextAction == "" && strings.TrimSpace(d.Reason) == "" {
		return decisionErr("decision_missing")
	}
	if ctx.Now.IsZero() {
		return decisionErr("invalid_decision_context")
	}
	if len(d.Summary) == 0 || len(d.Summary) > 4096 || len(d.Reason) > 2048 || len(d.ExpectedProgress) > 2048 {
		return decisionErr("invalid_decision_size")
	}
	if len(d.Evidence) > 32 {
		return decisionErr("invalid_decision_size")
	}
	for _, evidence := range d.Evidence {
		if len(evidence) > 2048 {
			return decisionErr("invalid_decision_size")
		}
	}
	switch d.Outcome {
	case OutcomeProgress:
		if strings.TrimSpace(d.ExpectedProgress) == "" {
			return decisionErr("invalid_decision_expected_progress")
		}
	case OutcomeNoChange, OutcomeCompleted, OutcomeBlocked:
	default:
		return decisionErr("invalid_decision_outcome")
	}
	if strings.TrimSpace(d.Reason) == "" {
		return decisionErr("invalid_decision_reason")
	}
	switch d.NextAction {
	case NextNone:
		if d.NextWakeAt != nil || d.Event != nil {
			return decisionErr("invalid_decision_next_action")
		}
	case NextNeedsInput:
		if d.Outcome == OutcomeCompleted {
			return decisionErr("completed_must_stop")
		}
		if d.NextWakeAt != nil || d.Event != nil {
			return decisionErr("invalid_decision_next_action")
		}
	case NextAt:
		if d.Outcome == OutcomeCompleted {
			return decisionErr("completed_must_stop")
		}
		if d.NextWakeAt == nil || !d.NextWakeAt.After(ctx.Now) {
			return decisionErr("invalid_decision_next_wake")
		}
		if strings.TrimSpace(d.ExpectedProgress) == "" {
			return decisionErr("invalid_decision_expected_progress")
		}
		if ctx.MinInterval > 0 && d.NextWakeAt.Before(ctx.Now.Add(ctx.MinInterval)) {
			return decisionErr("next_wake_before_min_interval")
		}
		if ctx.ExpiresAt != nil && !d.NextWakeAt.Before(*ctx.ExpiresAt) {
			return decisionErr("next_wake_after_expiry")
		}
	case NextEvent:
		if d.Outcome == OutcomeCompleted || d.Event == nil || strings.TrimSpace(d.Event.SourceID) == "" {
			return decisionErr("invalid_decision_event")
		}
		if !ctx.RegisteredSource[d.Event.SourceID] {
			return decisionErr("event_source_not_registered")
		}
		if !validEventFilter(d.Event.Filter, 0) {
			return decisionErr("invalid_decision_event_filter")
		}
		if encoded, err := json.Marshal(d.Event.Filter); err != nil || len(encoded) > 16*1024 {
			return decisionErr("invalid_decision_size")
		}
		if strings.TrimSpace(d.ExpectedProgress) == "" {
			return decisionErr("invalid_decision_expected_progress")
		}
	default:
		return decisionErr("invalid_decision_next_action")
	}
	return nil
}

func validEventFilter(v any, depth int) bool {
	if depth == 0 {
		m, ok := v.(map[string]any)
		if !ok || len(m) != 1 {
			return false
		}
		eq, ok := m["equals"].(map[string]any)
		if !ok || len(eq) == 0 || len(eq) > 32 {
			return false
		}
		for key, value := range eq {
			if strings.TrimSpace(key) == "" || !validEventFilter(value, 1) {
				return false
			}
		}
		return true
	}
	if depth > 3 {
		return false
	}
	switch x := v.(type) {
	case string, bool, int, int64:
		return true
	case float64:
		return !math.IsNaN(x) && !math.IsInf(x, 0)
	default:
		return false
	}
}

func (d FinalDecision) Clone() FinalDecision {
	out := d
	out.Evidence = append([]string(nil), d.Evidence...)
	if d.NextWakeAt != nil {
		t := *d.NextWakeAt
		out.NextWakeAt = &t
	}
	if d.Event != nil {
		out.Event = &EventSpec{SourceID: d.Event.SourceID, Filter: cloneDecisionMap(d.Event.Filter)}
	}
	return out
}

func cloneDecisionMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneDecisionValue(v)
	}
	return out
}
func cloneDecisionValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneDecisionMap(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = cloneDecisionValue(item)
		}
		return out
	default:
		return v
	}
}

// ScheduleIntent integration should consume a validated Clone of this value
// from ObserveTurn/FinishRun, then persist the Run terminal state and intent
// in one durable transaction. Validation itself must remain side-effect free.
