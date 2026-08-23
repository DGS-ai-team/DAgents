package turn

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// EventType is the durable lifecycle event name. High-frequency stream deltas
// are intentionally not required to use this journal contract.
type EventType string

const (
	EventTurnStarted         EventType = "turn.started"
	EventTurnInputAccepted   EventType = "turn.input.accepted"
	EventTurnSnapshotCreated EventType = "turn.snapshot.created"
	EventModelContextChanged EventType = "model.context.changed"
	EventTurnCompleted       EventType = "turn.completed"
	EventTurnFailed          EventType = "turn.failed"
	EventTurnCancelled       EventType = "turn.cancelled"
	EventTurnInterrupted     EventType = "turn.interrupted"
	EventTurnBudgetExhausted EventType = "turn.budget_exhausted"

	EventStepStarted              EventType = "step.started"
	EventStepResumed              EventType = "step.resumed"
	EventStepCompleted            EventType = "step.completed"
	EventStepFailed               EventType = "step.failed"
	EventStepCancelled            EventType = "step.cancelled"
	EventStepInterrupted          EventType = "step.interrupted"
	EventStepSuspended            EventType = "step.suspended"
	EventModelRequestStarted      EventType = "model.request.started"
	EventModelRequestCompleted    EventType = "model.request.completed"
	EventModelRequestFailed       EventType = "model.request.failed"
	EventModelRequestRetrying     EventType = "model.request.retrying"
	EventModelUsageRecorded       EventType = "model.usage.recorded"
	EventAssistantMessageRecorded EventType = "assistant.message.recorded"

	EventToolBatchCreated         EventType = "tool.batch.created"
	EventToolBatchSettled         EventType = "tool.batch.settled"
	EventToolCallRecorded         EventType = "tool.call.recorded"
	EventToolInteractionRequested EventType = "tool.interaction.requested"
	EventToolExecutionStarted     EventType = "tool.execution.started"
	EventToolExecutionRetrying    EventType = "tool.execution.retrying"
	EventToolExecutionCompleted   EventType = "tool.execution.completed"
	EventToolExecutionFailed      EventType = "tool.execution.failed"
	EventToolExecutionReconciled  EventType = "tool.execution.reconciled"
	EventToolResultRecorded       EventType = "tool.result.recorded"
	EventExternalFactRecorded     EventType = "external.fact.recorded"

	EventInteractionRequested EventType = "interaction.requested"
	EventInteractionResolved  EventType = "interaction.resolved"
	EventContextCompacted     EventType = "context.compacted"
)

var lifecycleEventTypes = map[EventType]struct{}{
	EventTurnStarted: {}, EventTurnInputAccepted: {}, EventTurnSnapshotCreated: {}, EventModelContextChanged: {},
	EventTurnCompleted: {}, EventTurnFailed: {}, EventTurnCancelled: {},
	EventTurnInterrupted: {}, EventTurnBudgetExhausted: {}, EventStepStarted: {},
	EventStepResumed: {}, EventStepCompleted: {}, EventStepFailed: {},
	EventStepCancelled: {}, EventStepInterrupted: {}, EventStepSuspended: {},
	EventModelRequestStarted: {}, EventModelRequestCompleted: {}, EventModelRequestFailed: {},
	EventModelRequestRetrying: {}, EventModelUsageRecorded: {}, EventAssistantMessageRecorded: {},
	EventToolBatchCreated: {}, EventToolBatchSettled: {}, EventToolCallRecorded: {},
	EventToolInteractionRequested: {}, EventToolExecutionStarted: {},
	EventToolExecutionRetrying: {}, EventToolExecutionCompleted: {},
	EventToolExecutionFailed: {}, EventToolExecutionReconciled: {}, EventToolResultRecorded: {},
	EventExternalFactRecorded: {},
	EventInteractionRequested: {}, EventInteractionResolved: {}, EventContextCompacted: {},
}

func (e EventType) Valid() bool {
	_, ok := lifecycleEventTypes[e]
	return ok
}

// TurnEventEnvelope is the domain contract for durable Turn/Step lifecycle
// events. Store-specific SQL records should map to this type rather than
// making the state machine depend on SQLite details.
type TurnEventEnvelope struct {
	ID              int64
	AgentID         string
	SessionID       string
	TurnID          string
	StepID          string
	ToolBatchID     string
	ToolCallID      string
	ToolExecutionID string
	InteractionID   string

	SessionSeq   uint64
	TurnSeq      uint64
	EventType    EventType
	EventVersion int
	Source       string
	CommandID    string
	Payload      json.RawMessage
	PayloadRef   string
	CreatedAt    time.Time
}

func NewTurnEventEnvelope(sessionID string, eventType EventType, now time.Time) TurnEventEnvelope {
	return TurnEventEnvelope{
		SessionID:    strings.TrimSpace(sessionID),
		EventType:    eventType,
		EventVersion: 1,
		CreatedAt:    now,
	}
}

func (e TurnEventEnvelope) Validate() error {
	if strings.TrimSpace(e.SessionID) == "" {
		return fmt.Errorf("event session id is required")
	}
	if !e.EventType.Valid() {
		return fmt.Errorf("unknown event type %q", e.EventType)
	}
	if e.EventVersion <= 0 {
		return fmt.Errorf("event version must be positive")
	}
	if e.SessionSeq == 0 {
		return fmt.Errorf("event session sequence must be positive")
	}
	if e.TurnSeq == 0 {
		return fmt.Errorf("event turn sequence must be positive")
	}
	if e.CreatedAt.IsZero() {
		return fmt.Errorf("event created_at is required")
	}
	if len(e.Payload) > 0 && !json.Valid(e.Payload) {
		return fmt.Errorf("event payload is invalid JSON")
	}
	return nil
}

// ValidateAfter checks the monotonic sequence invariant for a following event.
func (e TurnEventEnvelope) ValidateAfter(previous TurnEventEnvelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.SessionID != previous.SessionID {
		return fmt.Errorf("event session sequence crosses sessions")
	}
	if e.TurnID != previous.TurnID {
		return fmt.Errorf("event turn sequence crosses turns")
	}
	if e.SessionSeq != previous.SessionSeq+1 {
		return fmt.Errorf("event session sequence is not contiguous: previous=%d current=%d", previous.SessionSeq, e.SessionSeq)
	}
	if e.TurnSeq != previous.TurnSeq+1 {
		return fmt.Errorf("event turn sequence is not contiguous: previous=%d current=%d", previous.TurnSeq, e.TurnSeq)
	}
	return nil
}
