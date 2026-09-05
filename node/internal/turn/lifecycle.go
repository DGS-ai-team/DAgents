package turn

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBudgetExhausted is returned at an execution edge when the next model or
// tool side effect would exceed the active Turn budget. The session adapter
// converts it into a durable terminal Turn event instead of a generic error.
var ErrBudgetExhausted = errors.New("turn budget exhausted")

// TurnSource identifies the input that opened a logical Turn.
type TurnSource string

const (
	TurnSourceHuman      TurnSource = "human"
	TurnSourceTrigger    TurnSource = "trigger"
	TurnSourceChildAgent TurnSource = "child_agent"
	TurnSourceSideEffect TurnSource = "side_effect"
)

// TurnStatus is the durable state of a logical user-goal cycle.
type TurnStatus string

const (
	TurnStatusCreated         TurnStatus = "created"
	TurnStatusRunning         TurnStatus = "running"
	TurnStatusWaiting         TurnStatus = "waiting"
	TurnStatusCompleted       TurnStatus = "completed"
	TurnStatusFailed          TurnStatus = "failed"
	TurnStatusCancelled       TurnStatus = "cancelled"
	TurnStatusInterrupted     TurnStatus = "interrupted"
	TurnStatusBudgetExhausted TurnStatus = "budget_exhausted"
)

func (s TurnStatus) Terminal() bool {
	switch s {
	case TurnStatusCompleted, TurnStatusFailed, TurnStatusCancelled,
		TurnStatusInterrupted, TurnStatusBudgetExhausted:
		return true
	default:
		return false
	}
}

// StepStatus is the durable state of one model request and its tool batch.
type StepStatus string

const (
	StepStatusCreated            StepStatus = "created"
	StepStatusRequesting         StepStatus = "requesting"
	StepStatusAssistantReceived  StepStatus = "assistant_received"
	StepStatusExecutingTools     StepStatus = "executing_tools"
	StepStatusWaitingInteraction StepStatus = "waiting_for_interaction"
	StepStatusReadyForNext       StepStatus = "ready_for_next"
	StepStatusCompleted          StepStatus = "completed"
	StepStatusFailed             StepStatus = "failed"
	StepStatusCancelled          StepStatus = "cancelled"
	StepStatusInterrupted        StepStatus = "interrupted"
)

func (s StepStatus) Terminal() bool {
	switch s {
	case StepStatusCompleted, StepStatusFailed, StepStatusCancelled, StepStatusInterrupted:
		return true
	default:
		return false
	}
}

// ModelAttemptStatus describes one concrete provider request attempt.
type ModelAttemptStatus string

const (
	ModelAttemptStatusRunning     ModelAttemptStatus = "running"
	ModelAttemptStatusCompleted   ModelAttemptStatus = "completed"
	ModelAttemptStatusFailed      ModelAttemptStatus = "failed"
	ModelAttemptStatusInterrupted ModelAttemptStatus = "interrupted"
)

// ToolExecutionStatus describes the state of one ToolCall execution.
type ToolExecutionStatus string

const (
	ToolExecutionStatusProposed  ToolExecutionStatus = "proposed"
	ToolExecutionStatusPending   ToolExecutionStatus = "pending"
	ToolExecutionStatusRunning   ToolExecutionStatus = "running"
	ToolExecutionStatusSucceeded ToolExecutionStatus = "succeeded"
	ToolExecutionStatusFailed    ToolExecutionStatus = "failed"
	ToolExecutionStatusDenied    ToolExecutionStatus = "denied"
	ToolExecutionStatusCancelled ToolExecutionStatus = "cancelled"
	ToolExecutionStatusTimedOut  ToolExecutionStatus = "timed_out"
	ToolExecutionStatusUnknown   ToolExecutionStatus = "unknown"
)

func (s ToolExecutionStatus) Terminal() bool {
	switch s {
	case ToolExecutionStatusSucceeded, ToolExecutionStatusFailed, ToolExecutionStatusDenied,
		ToolExecutionStatusCancelled, ToolExecutionStatusTimedOut, ToolExecutionStatusUnknown:
		return true
	default:
		return false
	}
}

// InteractionStatus is the CAS-protected state of a pending HITL interaction.
type InteractionStatus string

const (
	InteractionStatusPending   InteractionStatus = "pending"
	InteractionStatusResolved  InteractionStatus = "resolved"
	InteractionStatusRejected  InteractionStatus = "rejected"
	InteractionStatusExpired   InteractionStatus = "expired"
	InteractionStatusCancelled InteractionStatus = "cancelled"
)

// TurnBudget contains the sole hard execution limits for one logical Turn.
type TurnBudget struct {
	MaxSteps            int           `json:"max_steps"`
	MaxToolCalls        int           `json:"max_tool_calls"`
	MaxToolRetries      int           `json:"max_tool_retries"`
	MaxWallTime         time.Duration `json:"max_wall_time_ns"`
	MaxInputTokens      int           `json:"max_input_tokens"`
	MaxOutputTokens     int           `json:"max_output_tokens"`
	MaxCost             float64       `json:"max_cost"`
	ReserveFinalSummary bool          `json:"reserve_final_summary"`
}

// StepUsage is deliberately provider-neutral. Provider-specific usage can be
// retained in an event payload without coupling the lifecycle model to llm.
type StepUsage struct {
	InputTokens                int     `json:"input_tokens"`
	OutputTokens               int     `json:"output_tokens"`
	TotalTokens                int     `json:"total_tokens"`
	PromptCacheHitTokens       int     `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens      int     `json:"prompt_cache_miss_tokens"`
	PromptCacheMetricsObserved bool    `json:"prompt_cache_metrics_observed"`
	ReasoningTokens            int     `json:"reasoning_tokens"`
	Cost                       float64 `json:"cost"`
}

// TurnUsage is the cumulative, provider-neutral accounting used for hard
// execution budgets. It is kept separate from StepUsage so a Turn spanning
// several model/tool Steps can be resumed and audited without re-counting
// provider events.
type TurnUsage struct {
	Steps                      int     `json:"steps"`
	ToolCalls                  int     `json:"tool_calls"`
	ToolRetries                int     `json:"tool_retries"`
	InputTokens                int     `json:"input_tokens"`
	OutputTokens               int     `json:"output_tokens"`
	TotalTokens                int     `json:"total_tokens"`
	PromptCacheHitTokens       int     `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens      int     `json:"prompt_cache_miss_tokens"`
	PromptCacheMetricsObserved bool    `json:"prompt_cache_metrics_observed"`
	ReasoningTokens            int     `json:"reasoning_tokens"`
	SummarySteps               int     `json:"summary_steps"`
	Cost                       float64 `json:"cost"`
}

// Turn is the logical user-goal cycle. It is intentionally independent from
// the current session runtime so it can later be persisted and projected.
type Turn struct {
	ID           string
	SessionID    string
	AgentID      string
	ParentTurnID string
	Source       TurnSource

	Status        TurnStatus
	EndReason     string
	CurrentStepID string
	StepIndex     int

	RuntimeRevision int64
	// ContextEpoch identifies the model-visible context segment. It advances
	// when a new ModelContextSnapshot is accepted, not when compaction merely
	// rewrites durable history before that segment is rebuilt.
	ContextEpoch    int
	ContextSnapshot *ModelContextSnapshot
	Budget          TurnBudget
	Usage           TurnUsage

	StartedAt  time.Time
	FinishedAt *time.Time
}

// BudgetDecision is a read-only preflight result for a lifecycle edge.
type BudgetDecision struct {
	Allowed bool
	Reason  string
	Usage   TurnUsage
	Budget  TurnBudget
}

// Step is one model request plus the ToolBatch that it produces.
type Step struct {
	ID           string
	TurnID       string
	Index        int
	Status       StepStatus
	EndReason    string
	ContextEpoch int

	RequestAttempt int
	ModelRequestID string
	AssistantMsgID string
	ToolBatchID    string
	FinalSummary   bool

	StartedAt  time.Time
	FinishedAt *time.Time
	Usage      StepUsage
}

// ToolBatch is the ordered set of ToolCalls emitted by one assistant
// message. Execution may be concurrent, but the batch is not settled until
// every known ToolExecution reaches a terminal state.
type ToolBatch struct {
	ID              string
	StepID          string
	ParallelAllowed bool
	Status          string
	ToolCallIDs     []string
}

// ToolExecution is the recoverable execution record for one ToolCall. An
// execution attempt can be retried without creating another ToolCall.
type ToolExecution struct {
	ID             string
	ToolBatchID    string
	ToolCallID     string
	ToolName       string
	Arguments      json.RawMessage
	Status         ToolExecutionStatus
	Attempt        int
	PolicyDecision string
	ApprovalID     string
	ResultRef      string
	ErrorKind      string
	StartedAt      time.Time
	FinishedAt     *time.Time
}

// PendingInteraction is the durable form of an external decision required
// before a ToolExecution can continue. PendingHITL remains the wire/history
// wire/history projection used by the runtime and API.
type PendingInteraction struct {
	ID              string
	TurnID          string
	StepID          string
	ToolExecutionID string
	Kind            string
	Status          InteractionStatus
	Payload         json.RawMessage
	Resolution      json.RawMessage
	Revision        int64
	ExpiresAt       *time.Time
}

// ModelAttempt is a retryable provider request within one logical Step.
type ModelAttempt struct {
	ID            string
	StepID        string
	Attempt       int
	RequestDigest string
	Status        ModelAttemptStatus
	ErrorKind     string
	Usage         StepUsage
	StartedAt     time.Time
	FinishedAt    *time.Time
}

func NewTurn(id, sessionID, agentID string, source TurnSource) (*Turn, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("turn id is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session id is required")
	}
	return &Turn{
		ID:           strings.TrimSpace(id),
		SessionID:    strings.TrimSpace(sessionID),
		AgentID:      strings.TrimSpace(agentID),
		Source:       source,
		Status:       TurnStatusCreated,
		ContextEpoch: 0,
	}, nil
}

func NewStep(id, turnID string, index, contextEpoch int) (*Step, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("step id is required")
	}
	if strings.TrimSpace(turnID) == "" {
		return nil, fmt.Errorf("turn id is required")
	}
	if index < 1 {
		return nil, fmt.Errorf("step index must be positive")
	}
	if contextEpoch < 0 {
		return nil, fmt.Errorf("context epoch cannot be negative")
	}
	return &Step{
		ID:           strings.TrimSpace(id),
		TurnID:       strings.TrimSpace(turnID),
		Index:        index,
		Status:       StepStatusCreated,
		ContextEpoch: contextEpoch,
	}, nil
}

// StartStep creates the next Step owned by a running Turn.
func (t *Turn) StartStep(stepID string, now time.Time) (*Step, error) {
	if t == nil {
		return nil, fmt.Errorf("turn is nil")
	}
	if t.Status != TurnStatusRunning {
		return nil, fmt.Errorf("cannot start step while turn is %s", t.Status)
	}
	step, err := NewStep(stepID, t.ID, t.StepIndex+1, t.ContextEpoch)
	if err != nil {
		return nil, err
	}
	t.StepIndex = step.Index
	t.CurrentStepID = step.ID
	if !now.IsZero() && t.StartedAt.IsZero() {
		t.StartedAt = now
	}
	return step, nil
}

// Advance applies a validated lifecycle event to a Turn.
func (t *Turn) Advance(event EventType, now time.Time, reason string) error {
	if t == nil {
		return fmt.Errorf("turn is nil")
	}
	next, ok := NextTurnStatus(t.Status, event)
	if !ok {
		return invalidTransition("turn", string(t.Status), string(event))
	}
	t.Status = next
	if strings.TrimSpace(reason) != "" {
		t.EndReason = strings.TrimSpace(reason)
	}
	if event == EventTurnStarted && !now.IsZero() && t.StartedAt.IsZero() {
		t.StartedAt = now
	}
	if next.Terminal() && !now.IsZero() {
		finished := now
		t.FinishedAt = &finished
	}
	return nil
}

// Advance applies a validated lifecycle event to a Step.
func (s *Step) Advance(event EventType, now time.Time, reason string) error {
	if s == nil {
		return fmt.Errorf("step is nil")
	}
	next, ok := NextStepStatus(s.Status, event)
	if !ok {
		return invalidTransition("step", string(s.Status), string(event))
	}
	s.Status = next
	if strings.TrimSpace(reason) != "" {
		s.EndReason = strings.TrimSpace(reason)
	}
	if event == EventStepStarted && !now.IsZero() && s.StartedAt.IsZero() {
		s.StartedAt = now
	}
	if next.Terminal() && !now.IsZero() {
		finished := now
		s.FinishedAt = &finished
	}
	return nil
}

func NextTurnStatus(current TurnStatus, event EventType) (TurnStatus, bool) {
	switch current {
	case TurnStatusCreated:
		switch event {
		case EventTurnStarted, EventTurnInputAccepted:
			return TurnStatusRunning, true
		case EventTurnCancelled:
			return TurnStatusCancelled, true
		}
	case TurnStatusRunning:
		switch event {
		case EventStepStarted, EventTurnSnapshotCreated, EventModelContextChanged, EventStepCompleted, EventModelRequestStarted,
			EventModelRequestCompleted, EventToolBatchCreated, EventToolResultRecorded:
			return TurnStatusRunning, true
		case EventStepSuspended, EventInteractionRequested:
			return TurnStatusWaiting, true
		case EventInteractionResolved:
			return TurnStatusRunning, true
		case EventTurnCompleted:
			return TurnStatusCompleted, true
		case EventTurnFailed:
			return TurnStatusFailed, true
		case EventTurnCancelled:
			return TurnStatusCancelled, true
		case EventTurnInterrupted:
			return TurnStatusInterrupted, true
		case EventTurnBudgetExhausted:
			return TurnStatusBudgetExhausted, true
		}
	case TurnStatusWaiting:
		switch event {
		case EventInteractionResolved, EventStepResumed:
			return TurnStatusRunning, true
		case EventContextCompacted:
			return TurnStatusWaiting, true
		case EventTurnCancelled:
			return TurnStatusCancelled, true
		case EventTurnInterrupted:
			return TurnStatusInterrupted, true
		case EventTurnFailed:
			return TurnStatusFailed, true
		}
	}
	return current, false
}

func NextStepStatus(current StepStatus, event EventType) (StepStatus, bool) {
	switch current {
	case StepStatusCreated:
		if event == EventStepStarted || event == EventModelRequestStarted {
			return StepStatusRequesting, true
		}
		if event == EventModelContextChanged {
			return StepStatusCreated, true
		}
		if event == EventStepCancelled {
			return StepStatusCancelled, true
		}
	case StepStatusRequesting:
		switch event {
		case EventModelRequestStarted:
			return StepStatusRequesting, true
		case EventModelContextChanged:
			return StepStatusRequesting, true
		case EventAssistantMessageRecorded, EventModelRequestCompleted:
			return StepStatusAssistantReceived, true
		case EventModelRequestRetrying, EventContextCompacted:
			return StepStatusRequesting, true
		case EventModelRequestFailed:
			return StepStatusFailed, true
		case EventStepInterrupted:
			return StepStatusInterrupted, true
		case EventStepCancelled:
			return StepStatusCancelled, true
		}
	case StepStatusAssistantReceived:
		switch event {
		case EventToolBatchCreated:
			return StepStatusExecutingTools, true
		case EventStepCompleted:
			return StepStatusCompleted, true
		case EventStepFailed:
			return StepStatusFailed, true
		case EventStepCancelled:
			return StepStatusCancelled, true
		}
	case StepStatusExecutingTools:
		switch event {
		case EventInteractionRequested:
			return StepStatusWaitingInteraction, true
		case EventToolResultRecorded:
			return StepStatusExecutingTools, true
		case EventToolBatchSettled:
			return StepStatusReadyForNext, true
		case EventStepCompleted:
			return StepStatusCompleted, true
		case EventStepFailed:
			return StepStatusFailed, true
		case EventStepInterrupted:
			return StepStatusInterrupted, true
		case EventStepCancelled:
			return StepStatusCancelled, true
		}
	case StepStatusWaitingInteraction:
		switch event {
		case EventContextCompacted:
			return StepStatusWaitingInteraction, true
		case EventInteractionResolved:
			return StepStatusExecutingTools, true
		case EventStepInterrupted:
			return StepStatusInterrupted, true
		case EventStepCancelled:
			return StepStatusCancelled, true
		}
	case StepStatusReadyForNext:
		switch event {
		case EventStepCompleted:
			return StepStatusCompleted, true
		case EventStepCancelled:
			return StepStatusCancelled, true
		}
	}
	return current, false
}

func invalidTransition(kind, current, event string) error {
	return fmt.Errorf("invalid %s transition: state=%s event=%s", kind, current, event)
}
