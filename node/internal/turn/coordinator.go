package turn

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// CommandType is the internal command vocabulary used by the TurnCoordinator.
// Queue-specific request types are translated into these commands at the
// SessionRuntime boundary.
type CommandType string

const (
	CommandStartTurn               CommandType = "start_turn"
	CommandStartStep               CommandType = "start_step"
	CommandTurnSnapshotCreated     CommandType = "turn_snapshot_created"
	CommandModelContextChanged     CommandType = "model_context_changed"
	CommandModelRequestStarted     CommandType = "model_request_started"
	CommandModelResponseCompleted  CommandType = "model_response_completed"
	CommandModelRequestFailed      CommandType = "model_request_failed"
	CommandModelRequestRetrying    CommandType = "model_request_retrying"
	CommandModelUsageRecorded      CommandType = "model_usage_recorded"
	CommandAssistantReceived       CommandType = "assistant_received"
	CommandToolBatchCreated        CommandType = "tool_batch_created"
	CommandToolBatchSettled        CommandType = "tool_batch_settled"
	CommandInteractionRequested    CommandType = "interaction_requested"
	CommandInteractionResolved     CommandType = "interaction_resolved"
	CommandCompleteStep            CommandType = "complete_step"
	CommandFailStep                CommandType = "fail_step"
	CommandInterruptStep           CommandType = "interrupt_step"
	CommandCancelStep              CommandType = "cancel_step"
	CommandCompleteTurn            CommandType = "complete_turn"
	CommandFailTurn                CommandType = "fail_turn"
	CommandInterruptTurn           CommandType = "interrupt_turn"
	CommandCancelTurn              CommandType = "cancel_turn"
	CommandBudgetExhausted         CommandType = "budget_exhausted"
	CommandContextCompacted        CommandType = "context_compacted"
	CommandExternalFactRecorded    CommandType = "external_fact_recorded"
	CommandToolCallRecorded        CommandType = "tool_call_recorded"
	CommandToolExecutionStarted    CommandType = "tool_execution_started"
	CommandToolExecutionRetrying   CommandType = "tool_execution_retrying"
	CommandToolExecutionCompleted  CommandType = "tool_execution_completed"
	CommandToolExecutionFailed     CommandType = "tool_execution_failed"
	CommandToolExecutionReconciled CommandType = "tool_execution_reconciled"
	CommandToolResultRecorded      CommandType = "tool_result_recorded"
)

// TurnCommand is deliberately independent from queue.Envelope. It is the
// stable runtime contract that a future queue, HTTP API, or replay driver can
// target without knowing current request_type names.
type TurnCommand struct {
	Type                 CommandType
	CommandID            string
	SessionID            string
	TurnID               string
	StepID               string
	Generation           uint64
	Source               TurnSource
	At                   time.Time
	Reason               string
	HasTools             bool
	ToolBatchID          string
	FinalSummary         bool
	ToolCallID           string
	ToolExecutionID      string
	ExternalFactID       string
	ExternalFactKind     string
	ToolName             string
	Arguments            json.RawMessage
	InteractionID        string
	InteractionKind      string
	InteractionRevision  int64
	RequestDigest        string
	AssistantMessageID   string
	RuntimeRevision      int64
	RuntimeDigest        string
	PromptDigest         string
	ToolDigest           string
	ContextSnapshot      *ModelContextSnapshot
	ContextBeforeDigest  string
	ContextAfterDigest   string
	CompactedMessageFrom int
	CompactedMessageTo   int
	Budget               TurnBudget
	ErrorKind            string
	Payload              json.RawMessage
	PayloadRef           string
	ResultContent        string
	ExecutionStatus      ToolExecutionStatus
	Usage                StepUsage
}

// CoordinatorSnapshot is a read-only projection of the coordinator's active
// Turn and Step. It carries compact lifecycle metadata and the serialized
// interaction payload needed by the runtime wire projection; full messages
// remain in their existing history store.
type CoordinatorSnapshot struct {
	SessionID          string
	TurnID             string
	StepID             string
	Generation         uint64
	TurnStatus         TurnStatus
	TurnEndReason      string
	StepStatus         StepStatus
	StepEndReason      string
	StepIndex          int
	ContextEpoch       int
	ToolBatchID        string
	FinalSummary       bool
	InteractionID      string
	InteractionKind    string
	InteractionPayload json.RawMessage
	ModelAttempt       int
	RuntimeRevision    int64
	RuntimeDigest      string
	PromptDigest       string
	ToolDigest         string
	AssistantMsgID     string
	ContextSnapshot    *ModelContextSnapshot
	RecoveryRequired   bool
	ExternalFacts      int
	ToolExecutions     []ToolExecutionView
	Budget             TurnBudget
	Usage              TurnUsage
	HasActiveTurn      bool
}

// ToolExecutionView is the safe, compact execution projection used by the
// UI. Arguments and result content stay in the lifecycle journal/history.
type ToolExecutionView struct {
	ID         string              `json:"id"`
	ToolCallID string              `json:"tool_call_id"`
	ToolName   string              `json:"tool_name"`
	Status     ToolExecutionStatus `json:"status"`
	Attempt    int                 `json:"attempt,omitempty"`
}

// TurnCoordinator owns the logical Turn/Step state machine for one Session.
// It is safe to call from a SessionRuntime boundary, but it does not execute
// LLMs or tools and therefore remains easy to test independently.
type TurnCoordinator struct {
	mu               sync.Mutex
	sessionID        string
	agentID          string
	generation       uint64
	turn             *Turn
	step             *Step
	batch            *ToolBatch
	executions       map[string]*ToolExecution
	toolResults      map[string]bool
	externalFacts    map[string]bool
	appliedCommands  map[string]bool
	interaction      *PendingInteraction
	attempts         []ModelAttempt
	recoveryRequired bool
}

func NewTurnCoordinator(sessionID, agentID string) *TurnCoordinator {
	return &TurnCoordinator{
		sessionID:     strings.TrimSpace(sessionID),
		agentID:       strings.TrimSpace(agentID),
		executions:    make(map[string]*ToolExecution),
		externalFacts: make(map[string]bool),
	}
}

// Dispatch applies one command and returns the resulting projection.
func (c *TurnCoordinator) Dispatch(command TurnCommand) (CoordinatorSnapshot, error) {
	if c == nil {
		return CoordinatorSnapshot{}, fmt.Errorf("turn coordinator is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dispatchLocked(command)
}

// DispatchDurable applies a command and gives the caller a chance to append
// the resulting fact before the projection becomes visible. If persistence
// fails, the coordinator rolls back to its exact pre-command state. This is
// the migration bridge that keeps the event log authoritative without making
// the store package depend on the state machine.
func (c *TurnCoordinator) DispatchDurable(command TurnCommand, persist func(TurnCommand, CoordinatorSnapshot) error) (CoordinatorSnapshot, error) {
	if c == nil {
		return CoordinatorSnapshot{}, fmt.Errorf("turn coordinator is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	checkpoint := c.checkpointLocked()
	snapshot, err := c.dispatchLocked(command)
	if err != nil {
		return snapshot, err
	}
	if persist != nil {
		if err := persist(command, snapshot); err != nil {
			c.restoreCheckpointLocked(checkpoint)
			return c.snapshotLocked(), err
		}
	}
	return snapshot, nil
}

func (c *TurnCoordinator) dispatchLocked(command TurnCommand) (CoordinatorSnapshot, error) {
	if commandID := strings.TrimSpace(command.CommandID); commandID != "" && c.appliedCommands[commandID] {
		return c.snapshotLocked(), nil
	}
	if err := c.validateCommandLocked(command); err != nil {
		return c.snapshotLocked(), err
	}
	if command.At.IsZero() {
		command.At = time.Now().UTC()
	}

	// A command handler may perform several state mutations before discovering
	// an invalid edge (for example interaction resolution validates the
	// revision after advancing the Turn). Keep Dispatch atomic even when the
	// caller is not using the durable store adapter.
	checkpoint := c.checkpointLocked()
	err := c.applyCommandLocked(command)
	if err != nil {
		c.restoreCheckpointLocked(checkpoint)
		return c.snapshotLocked(), err
	}
	if err == nil && strings.TrimSpace(command.CommandID) != "" {
		if c.appliedCommands == nil {
			c.appliedCommands = make(map[string]bool)
		}
		c.appliedCommands[command.CommandID] = true
	}
	return c.snapshotLocked(), err
}

type coordinatorCheckpoint struct {
	generation       uint64
	turn             *Turn
	step             *Step
	batch            *ToolBatch
	executions       map[string]*ToolExecution
	toolResults      map[string]bool
	externalFacts    map[string]bool
	appliedCommands  map[string]bool
	interaction      *PendingInteraction
	attempts         []ModelAttempt
	recoveryRequired bool
}

func (c *TurnCoordinator) checkpointLocked() coordinatorCheckpoint {
	checkpoint := coordinatorCheckpoint{
		generation:       c.generation,
		turn:             cloneTurn(c.turn),
		step:             cloneStep(c.step),
		batch:            cloneToolBatch(c.batch),
		executions:       cloneExecutions(c.executions),
		toolResults:      cloneStringSet(c.toolResults),
		externalFacts:    cloneStringSet(c.externalFacts),
		appliedCommands:  cloneStringSet(c.appliedCommands),
		interaction:      clonePendingInteraction(c.interaction),
		attempts:         append([]ModelAttempt(nil), c.attempts...),
		recoveryRequired: c.recoveryRequired,
	}
	for i := range checkpoint.attempts {
		checkpoint.attempts[i].Usage = c.attempts[i].Usage
	}
	return checkpoint
}

func (c *TurnCoordinator) restoreCheckpointLocked(checkpoint coordinatorCheckpoint) {
	c.generation = checkpoint.generation
	c.turn = checkpoint.turn
	c.step = checkpoint.step
	c.batch = checkpoint.batch
	c.executions = checkpoint.executions
	c.toolResults = checkpoint.toolResults
	c.externalFacts = checkpoint.externalFacts
	c.appliedCommands = checkpoint.appliedCommands
	c.interaction = checkpoint.interaction
	c.attempts = checkpoint.attempts
	c.recoveryRequired = checkpoint.recoveryRequired
}

func cloneTurn(value *Turn) *Turn {
	if value == nil {
		return nil
	}
	clone := *value
	clone.ContextSnapshot = value.ContextSnapshot.Clone()
	return &clone
}

func cloneStep(value *Step) *Step {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneToolBatch(value *ToolBatch) *ToolBatch {
	if value == nil {
		return nil
	}
	clone := *value
	clone.ToolCallIDs = append([]string(nil), value.ToolCallIDs...)
	return &clone
}

func cloneExecutions(values map[string]*ToolExecution) map[string]*ToolExecution {
	if values == nil {
		return make(map[string]*ToolExecution)
	}
	clones := make(map[string]*ToolExecution, len(values))
	for id, value := range values {
		if value == nil {
			continue
		}
		clone := *value
		clone.Arguments = append(json.RawMessage(nil), value.Arguments...)
		if value.FinishedAt != nil {
			finished := *value.FinishedAt
			clone.FinishedAt = &finished
		}
		clones[id] = &clone
	}
	return clones
}

func clonePendingInteraction(value *PendingInteraction) *PendingInteraction {
	if value == nil {
		return nil
	}
	clone := *value
	clone.Payload = append(json.RawMessage(nil), value.Payload...)
	clone.Resolution = append(json.RawMessage(nil), value.Resolution...)
	if value.ExpiresAt != nil {
		expires := *value.ExpiresAt
		clone.ExpiresAt = &expires
	}
	return &clone
}

func cloneStringSet(values map[string]bool) map[string]bool {
	if values == nil {
		return make(map[string]bool)
	}
	clone := make(map[string]bool, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func (c *TurnCoordinator) applyCommandLocked(command TurnCommand) error {
	switch command.Type {
	case CommandStartTurn:
		return c.startTurnLocked(command)
	case CommandStartStep:
		return c.startStepLocked(command)
	case CommandTurnSnapshotCreated:
		return c.snapshotCreatedLocked(command)
	case CommandModelContextChanged:
		return c.contextChangedLocked(command)
	case CommandModelRequestStarted:
		return c.startModelAttemptLocked(command)
	case CommandModelResponseCompleted:
		return c.completeModelAttemptLocked(command)
	case CommandAssistantReceived:
		return c.recordAssistantLocked(command)
	case CommandModelRequestFailed:
		return c.finishModelAttemptLocked(EventModelRequestFailed, ModelAttemptStatusFailed, command)
	case CommandModelRequestRetrying:
		return c.retryModelAttemptLocked(command)
	case CommandModelUsageRecorded:
		return c.recordModelUsageLocked(command)
	case CommandToolBatchCreated:
		return c.advanceStepLocked(EventToolBatchCreated, command)
	case CommandToolBatchSettled:
		return c.advanceStepLocked(EventToolBatchSettled, command)
	case CommandToolCallRecorded:
		return c.recordToolCallLocked(command)
	case CommandToolExecutionStarted, CommandToolExecutionRetrying,
		CommandToolExecutionCompleted, CommandToolExecutionFailed,
		CommandToolExecutionReconciled:
		return c.advanceToolExecutionLocked(command)
	case CommandExternalFactRecorded:
		return c.recordExternalFactLocked(command)
	case CommandToolResultRecorded:
		callID := strings.TrimSpace(command.ToolCallID)
		if callID != "" && c.toolResults[callID] {
			return nil
		}
		executionID := strings.TrimSpace(command.ToolExecutionID)
		if executionID == "" {
			return fmt.Errorf("tool result requires a tool execution id")
		}
		execution := c.executions[executionID]
		if execution == nil {
			return fmt.Errorf("unknown tool execution %s", executionID)
		}
		if !execution.Status.Terminal() || execution.Status == ToolExecutionStatusUnknown {
			return fmt.Errorf("tool result requires a known terminal execution %s", executionID)
		}
		err := c.advanceStepLocked(EventToolResultRecorded, command)
		if err == nil && callID != "" {
			c.toolResults[callID] = true
		}
		return err
	case CommandInteractionRequested:
		return c.interactionRequestedLocked(command)
	case CommandInteractionResolved:
		return c.interactionResolvedLocked(command)
	case CommandCompleteStep:
		return c.advanceStepLocked(EventStepCompleted, command)
	case CommandFailStep:
		return c.advanceStepLocked(EventStepFailed, command)
	case CommandInterruptStep:
		return c.advanceStepLocked(EventStepInterrupted, command)
	case CommandCancelStep:
		return c.advanceStepLocked(EventStepCancelled, command)
	case CommandCompleteTurn:
		return c.advanceTurnLocked(EventTurnCompleted, command)
	case CommandFailTurn:
		return c.advanceTurnLocked(EventTurnFailed, command)
	case CommandInterruptTurn:
		return c.advanceTurnLocked(EventTurnInterrupted, command)
	case CommandCancelTurn:
		return c.advanceTurnLocked(EventTurnCancelled, command)
	case CommandBudgetExhausted:
		return c.advanceTurnLocked(EventTurnBudgetExhausted, command)
	case CommandContextCompacted:
		return c.contextCompactedLocked(command)
	default:
		return fmt.Errorf("unknown turn command %q", command.Type)
	}
}

// Restore replays persisted lifecycle events into the in-memory projection.
// It intentionally uses the same command handlers as live execution so
// recovery cannot silently invent a second state machine.
func (c *TurnCoordinator) Restore(events []TurnEventEnvelope) error {
	if c == nil {
		return fmt.Errorf("turn coordinator is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	checkpoint := c.checkpointLocked()
	rollback := func(err error) error {
		c.restoreCheckpointLocked(checkpoint)
		return err
	}
	c.turn, c.step, c.batch, c.interaction, c.attempts = nil, nil, nil, nil, nil
	c.generation = 0
	c.recoveryRequired = false
	c.executions = make(map[string]*ToolExecution)
	c.toolResults = make(map[string]bool)
	c.externalFacts = make(map[string]bool)
	c.appliedCommands = make(map[string]bool)
	var previousSessionSeq uint64
	for _, event := range events {
		if event.SessionID != c.sessionID {
			return rollback(fmt.Errorf("event session mismatch: got=%s want=%s", event.SessionID, c.sessionID))
		}
		if previousSessionSeq != 0 && event.SessionSeq != previousSessionSeq+1 {
			return rollback(fmt.Errorf("event session sequence gap: previous=%d current=%d", previousSessionSeq, event.SessionSeq))
		}
		previousSessionSeq = event.SessionSeq
		command, ok, err := replayCommand(event)
		if err != nil {
			return rollback(err)
		}
		if !ok {
			continue
		}
		if err := c.validateCommandLocked(command); err != nil {
			return rollback(fmt.Errorf("replay %s: %w", event.EventType, err))
		}
		if err := c.applyCommandLocked(command); err != nil {
			return rollback(fmt.Errorf("replay %s: %w", event.EventType, err))
		}
		if event.CommandID != "" {
			c.appliedCommands[event.CommandID] = true
		}
	}
	return nil
}

func replayCommand(event TurnEventEnvelope) (TurnCommand, bool, error) {
	var meta struct {
		Generation           uint64                `json:"generation"`
		HasTools             bool                  `json:"has_tools"`
		FinalSummary         bool                  `json:"final_summary"`
		InteractionKind      string                `json:"interaction_kind"`
		InteractionRev       int64                 `json:"interaction_revision"`
		ToolBatchID          string                `json:"tool_batch_id"`
		ExternalFactID       string                `json:"external_fact_id"`
		ExternalFactKind     string                `json:"external_fact_kind"`
		InteractionID        string                `json:"interaction_id"`
		ToolName             string                `json:"tool_name"`
		ErrorKind            string                `json:"error_kind"`
		ExecutionStatus      ToolExecutionStatus   `json:"execution_status"`
		RuntimeRevision      int64                 `json:"runtime_revision"`
		RuntimeDigest        string                `json:"runtime_digest"`
		PromptDigest         string                `json:"prompt_digest"`
		ToolDigest           string                `json:"tool_digest"`
		RequestDigest        string                `json:"request_digest"`
		AssistantMessageID   string                `json:"assistant_message_id"`
		ArgumentsJSON        string                `json:"arguments_json"`
		ContextSnapshot      *ModelContextSnapshot `json:"context_snapshot"`
		InteractionPayload   json.RawMessage       `json:"interaction_payload"`
		ResultContent        string                `json:"result_content"`
		ContextBeforeDigest  string                `json:"context_before_digest"`
		ContextAfterDigest   string                `json:"context_after_digest"`
		CompactedMessageFrom int                   `json:"compacted_message_from"`
		CompactedMessageTo   int                   `json:"compacted_message_to"`
		Budget               TurnBudget            `json:"budget"`
		Usage                StepUsage             `json:"usage"`
	}
	if len(event.Payload) > 0 && string(event.Payload) != "null" {
		if err := json.Unmarshal(event.Payload, &meta); err != nil {
			return TurnCommand{}, false, err
		}
	}
	command := TurnCommand{
		SessionID: event.SessionID, TurnID: event.TurnID, StepID: event.StepID,
		Generation: meta.Generation, CommandID: event.CommandID, Source: TurnSource(event.Source),
		At: event.CreatedAt, ToolCallID: event.ToolCallID, ToolExecutionID: event.ToolExecutionID,
		ToolBatchID: meta.ToolBatchID, ToolName: meta.ToolName, InteractionID: event.InteractionID,
		ExternalFactID: meta.ExternalFactID, ExternalFactKind: meta.ExternalFactKind,
		FinalSummary:        meta.FinalSummary,
		InteractionKind:     meta.InteractionKind,
		InteractionRevision: meta.InteractionRev, ErrorKind: meta.ErrorKind,
		ExecutionStatus: meta.ExecutionStatus, PayloadRef: event.PayloadRef,
		ResultContent:        meta.ResultContent,
		ContextBeforeDigest:  meta.ContextBeforeDigest,
		ContextAfterDigest:   meta.ContextAfterDigest,
		CompactedMessageFrom: meta.CompactedMessageFrom,
		CompactedMessageTo:   meta.CompactedMessageTo,
		RuntimeRevision:      meta.RuntimeRevision, RuntimeDigest: meta.RuntimeDigest,
		PromptDigest: meta.PromptDigest, ToolDigest: meta.ToolDigest,
		RequestDigest:      meta.RequestDigest,
		AssistantMessageID: meta.AssistantMessageID,
		Budget:             meta.Budget,
		Usage:              meta.Usage,
		ContextSnapshot:    meta.ContextSnapshot,
		Payload:            meta.InteractionPayload,
	}
	if json.Valid([]byte(meta.ArgumentsJSON)) {
		command.Arguments = json.RawMessage(meta.ArgumentsJSON)
	}
	switch event.EventType {
	case EventTurnStarted:
		command.Type = CommandStartTurn
	case EventStepStarted:
		command.Type = CommandStartStep
	case EventTurnSnapshotCreated:
		command.Type = CommandTurnSnapshotCreated
	case EventModelContextChanged:
		command.Type = CommandModelContextChanged
	case EventModelRequestStarted:
		command.Type = CommandModelRequestStarted
	case EventModelRequestCompleted:
		command.Type = CommandModelResponseCompleted
	case EventModelRequestFailed:
		command.Type = CommandModelRequestFailed
	case EventModelRequestRetrying:
		command.Type = CommandModelRequestRetrying
	case EventModelUsageRecorded:
		command.Type = CommandModelUsageRecorded
	case EventAssistantMessageRecorded:
		command.Type = CommandAssistantReceived
		command.HasTools = meta.HasTools
	case EventToolBatchCreated:
		command.Type = CommandToolBatchCreated
	case EventToolBatchSettled:
		command.Type = CommandToolBatchSettled
	case EventToolCallRecorded:
		command.Type = CommandToolCallRecorded
	case EventToolExecutionStarted:
		command.Type = CommandToolExecutionStarted
	case EventToolExecutionRetrying:
		command.Type = CommandToolExecutionRetrying
	case EventToolExecutionCompleted:
		command.Type = CommandToolExecutionCompleted
	case EventToolExecutionFailed:
		command.Type = CommandToolExecutionFailed
	case EventToolExecutionReconciled:
		command.Type = CommandToolExecutionReconciled
	case EventToolResultRecorded:
		command.Type = CommandToolResultRecorded
	case EventExternalFactRecorded:
		command.Type = CommandExternalFactRecorded
	case EventInteractionRequested:
		command.Type = CommandInteractionRequested
	case EventInteractionResolved:
		command.Type = CommandInteractionResolved
	case EventContextCompacted:
		command.Type = CommandContextCompacted
	case EventStepCompleted:
		command.Type = CommandCompleteStep
	case EventStepFailed:
		command.Type = CommandFailStep
	case EventStepInterrupted:
		command.Type = CommandInterruptStep
	case EventStepCancelled:
		command.Type = CommandCancelStep
	case EventTurnCompleted:
		command.Type = CommandCompleteTurn
	case EventTurnFailed:
		command.Type = CommandFailTurn
	case EventTurnInterrupted:
		command.Type = CommandInterruptTurn
	case EventTurnCancelled:
		command.Type = CommandCancelTurn
	case EventTurnBudgetExhausted:
		command.Type = CommandBudgetExhausted
	default:
		return TurnCommand{}, false, nil
	}
	return command, true, nil
}

func (c *TurnCoordinator) validateCommandLocked(command TurnCommand) error {
	if strings.TrimSpace(c.sessionID) == "" {
		return fmt.Errorf("coordinator session id is required")
	}
	if strings.TrimSpace(command.SessionID) == "" {
		return fmt.Errorf("command session id is required")
	}
	if command.SessionID != c.sessionID {
		return fmt.Errorf("command session mismatch: got=%s want=%s", command.SessionID, c.sessionID)
	}
	if c.turn != nil && command.Type != CommandStartTurn {
		if strings.TrimSpace(command.TurnID) == "" {
			return fmt.Errorf("command turn id is required while turn is active")
		}
		if command.TurnID != c.turn.ID {
			return fmt.Errorf("command turn mismatch: got=%s want=%s", command.TurnID, c.turn.ID)
		}
		if command.Generation != 0 && command.Generation != c.generation {
			return fmt.Errorf("command generation mismatch: got=%d want=%d", command.Generation, c.generation)
		}
	}
	if command.Type != CommandStartTurn && c.turn == nil {
		return fmt.Errorf("command %q requires an active turn", command.Type)
	}
	if commandRequiresStep(command.Type) {
		if c.step == nil {
			return fmt.Errorf("command %q requires an active step", command.Type)
		}
		if strings.TrimSpace(command.StepID) == "" {
			return fmt.Errorf("command step id is required")
		}
		if command.StepID != c.step.ID {
			return fmt.Errorf("command step mismatch: got=%s want=%s", command.StepID, c.step.ID)
		}
	}
	return nil
}

func commandRequiresStep(command CommandType) bool {
	switch command {
	case CommandStartStep, CommandTurnSnapshotCreated, CommandCompleteTurn, CommandFailTurn,
		CommandInterruptTurn, CommandCancelTurn, CommandBudgetExhausted:
		return false
	default:
		return command != CommandStartTurn
	}
}

func (c *TurnCoordinator) startTurnLocked(command TurnCommand) error {
	if c.turn != nil && !c.turn.Status.Terminal() {
		return fmt.Errorf("active turn already exists: %s", c.turn.ID)
	}
	if strings.TrimSpace(command.TurnID) == "" {
		return fmt.Errorf("start turn requires turn id")
	}
	turnValue, err := NewTurn(command.TurnID, c.sessionID, c.agentID, command.Source)
	if err != nil {
		return err
	}
	c.turn = turnValue
	c.turn.Budget = command.Budget
	c.recoveryRequired = false
	c.externalFacts = make(map[string]bool)
	c.step = nil
	if command.Generation != 0 {
		c.generation = command.Generation
	} else {
		c.generation++
		if c.generation == 0 {
			c.generation = 1
		}
	}
	return c.turn.Advance(EventTurnStarted, command.At, command.Reason)
}

func (c *TurnCoordinator) startStepLocked(command TurnCommand) error {
	if c.turn == nil || c.turn.Status != TurnStatusRunning {
		return fmt.Errorf("cannot start step without a running turn")
	}
	if c.step != nil && !c.step.Status.Terminal() {
		return fmt.Errorf("active step already exists: %s", c.step.ID)
	}
	if strings.TrimSpace(command.StepID) == "" {
		return fmt.Errorf("start step requires step id")
	}
	stepValue, err := c.turn.StartStep(command.StepID, command.At)
	if err != nil {
		return err
	}
	c.step = stepValue
	c.step.FinalSummary = command.FinalSummary
	c.turn.Usage.Steps++
	if command.FinalSummary {
		c.turn.Usage.SummarySteps++
	}
	c.batch = nil
	c.executions = make(map[string]*ToolExecution)
	c.toolResults = make(map[string]bool)
	c.interaction = nil
	c.attempts = nil
	return c.step.Advance(EventStepStarted, command.At, command.Reason)
}

func (c *TurnCoordinator) snapshotCreatedLocked(command TurnCommand) error {
	if c.turn == nil {
		return fmt.Errorf("turn snapshot requires an active turn")
	}
	if command.ContextSnapshot != nil {
		if command.RuntimeRevision == 0 {
			command.RuntimeRevision = command.ContextSnapshot.RuntimeRevision
		}
		if command.RuntimeDigest == "" {
			command.RuntimeDigest = command.ContextSnapshot.RuntimeDigest
		}
		if command.PromptDigest == "" {
			command.PromptDigest = command.ContextSnapshot.PromptDigest
		}
		if command.ToolDigest == "" {
			command.ToolDigest = command.ContextSnapshot.ToolDigest
		}
	}
	if c.turn.ContextSnapshot != nil {
		// A repeated observer callback is harmless only when it describes the
		// same frozen model inputs. A different digest would violate Turn
		// immutability and must be rejected.
		if c.turn.ContextSnapshot.RuntimeDigest != command.RuntimeDigest ||
			c.turn.ContextSnapshot.PromptDigest != command.PromptDigest ||
			c.turn.ContextSnapshot.ToolDigest != command.ToolDigest {
			return fmt.Errorf("turn context snapshot already frozen with different digest")
		}
		return nil
	}
	c.turn.RuntimeRevision = command.RuntimeRevision
	if command.ContextSnapshot != nil {
		c.turn.ContextSnapshot = command.ContextSnapshot.Clone()
		if command.RuntimeRevision == 0 {
			c.turn.RuntimeRevision = command.ContextSnapshot.RuntimeRevision
		}
	} else {
		c.turn.ContextSnapshot = &ModelContextSnapshot{
			RuntimeRevision: command.RuntimeRevision,
			RuntimeDigest:   command.RuntimeDigest,
			PromptDigest:    command.PromptDigest,
			ToolDigest:      command.ToolDigest,
		}
	}
	return c.turn.Advance(EventTurnSnapshotCreated, command.At, command.Reason)
}

func (c *TurnCoordinator) contextChangedLocked(command TurnCommand) error {
	if c.turn == nil || c.step == nil {
		return fmt.Errorf("model context change requires an active step")
	}
	if command.ContextSnapshot == nil {
		return fmt.Errorf("model context change requires a snapshot")
	}
	if strings.TrimSpace(command.ContextSnapshot.PromptDigest) == "" || strings.TrimSpace(command.ContextSnapshot.ToolDigest) == "" {
		return fmt.Errorf("model context change requires prompt and tool digests")
	}
	c.turn.ContextSnapshot = command.ContextSnapshot.Clone()
	c.turn.RuntimeRevision = command.ContextSnapshot.RuntimeRevision
	c.turn.ContextEpoch++
	c.step.ContextEpoch = c.turn.ContextEpoch
	// The mutation reason belongs to the durable event payload. It is not a
	// terminal reason and must not overwrite the active Turn/Step end_reason.
	if err := c.turn.Advance(EventModelContextChanged, command.At, ""); err != nil {
		return err
	}
	return c.step.Advance(EventModelContextChanged, command.At, "")
}

func (c *TurnCoordinator) recordAssistantLocked(command TurnCommand) error {
	if c.step == nil {
		return fmt.Errorf("assistant response requires an active step")
	}
	if err := c.step.Advance(EventAssistantMessageRecorded, command.At, command.Reason); err != nil {
		return err
	}
	if strings.TrimSpace(command.AssistantMessageID) != "" {
		c.step.AssistantMsgID = strings.TrimSpace(command.AssistantMessageID)
	}
	if command.HasTools {
		batchID := strings.TrimSpace(command.ToolBatchID)
		if batchID == "" {
			batchID = c.step.ID + "-batch"
		}
		c.batch = &ToolBatch{ID: batchID, StepID: c.step.ID, Status: "created"}
		c.step.ToolBatchID = batchID
		return c.step.Advance(EventToolBatchCreated, command.At, command.Reason)
	}
	return nil
}

func (c *TurnCoordinator) startModelAttemptLocked(command TurnCommand) error {
	if c.step == nil {
		return fmt.Errorf("model request requires an active step")
	}
	if len(c.attempts) > 0 {
		last := &c.attempts[len(c.attempts)-1]
		if last.Status == ModelAttemptStatusRunning {
			return nil
		}
	}
	attemptNumber := len(c.attempts) + 1
	attemptBase := strings.TrimSpace(command.RequestDigest)
	if attemptBase == "" {
		attemptBase = c.step.ID
	}
	attemptID := fmt.Sprintf("%s-attempt-%d", attemptBase, attemptNumber)
	attempt := ModelAttempt{ID: attemptID, StepID: c.step.ID, Attempt: attemptNumber, RequestDigest: command.RequestDigest, Status: ModelAttemptStatusRunning, StartedAt: command.At}
	c.attempts = append(c.attempts, attempt)
	c.step.RequestAttempt = attemptNumber
	c.step.ModelRequestID = attemptID
	return c.step.Advance(EventModelRequestStarted, command.At, command.Reason)
}

func (c *TurnCoordinator) completeModelAttemptLocked(command TurnCommand) error {
	if len(c.attempts) > 0 {
		last := &c.attempts[len(c.attempts)-1]
		if last.Status == ModelAttemptStatusRunning {
			last.Status = ModelAttemptStatusCompleted
			finished := command.At
			last.FinishedAt = &finished
		}
	}
	return nil
}

func (c *TurnCoordinator) recordModelUsageLocked(command TurnCommand) error {
	if c.turn == nil || c.step == nil {
		return fmt.Errorf("model usage requires an active step")
	}
	if len(c.attempts) == 0 {
		return fmt.Errorf("model usage requires a model attempt")
	}
	last := &c.attempts[len(c.attempts)-1]
	delta := usageDelta(command.Usage, last.Usage)
	last.Usage = command.Usage
	c.step.Usage.InputTokens += delta.InputTokens
	c.step.Usage.OutputTokens += delta.OutputTokens
	c.step.Usage.TotalTokens += delta.TotalTokens
	c.step.Usage.PromptCacheHitTokens += delta.PromptCacheHitTokens
	c.step.Usage.PromptCacheMissTokens += delta.PromptCacheMissTokens
	c.step.Usage.PromptCacheMetricsObserved = c.step.Usage.PromptCacheMetricsObserved || delta.PromptCacheMetricsObserved
	c.step.Usage.ReasoningTokens += delta.ReasoningTokens
	c.step.Usage.Cost += delta.Cost
	c.turn.Usage.InputTokens += delta.InputTokens
	c.turn.Usage.OutputTokens += delta.OutputTokens
	c.turn.Usage.TotalTokens += delta.TotalTokens
	c.turn.Usage.PromptCacheHitTokens += delta.PromptCacheHitTokens
	c.turn.Usage.PromptCacheMissTokens += delta.PromptCacheMissTokens
	c.turn.Usage.PromptCacheMetricsObserved = c.turn.Usage.PromptCacheMetricsObserved || delta.PromptCacheMetricsObserved
	c.turn.Usage.ReasoningTokens += delta.ReasoningTokens
	c.turn.Usage.Cost += delta.Cost
	return nil
}

func usageDelta(current, previous StepUsage) StepUsage {
	delta := StepUsage{
		InputTokens:                current.InputTokens - previous.InputTokens,
		OutputTokens:               current.OutputTokens - previous.OutputTokens,
		TotalTokens:                current.TotalTokens - previous.TotalTokens,
		PromptCacheHitTokens:       current.PromptCacheHitTokens - previous.PromptCacheHitTokens,
		PromptCacheMissTokens:      current.PromptCacheMissTokens - previous.PromptCacheMissTokens,
		ReasoningTokens:            current.ReasoningTokens - previous.ReasoningTokens,
		PromptCacheMetricsObserved: current.PromptCacheMetricsObserved || previous.PromptCacheMetricsObserved,
		Cost:                       current.Cost - previous.Cost,
	}
	if delta.InputTokens < 0 {
		delta.InputTokens = current.InputTokens
	}
	if delta.OutputTokens < 0 {
		delta.OutputTokens = current.OutputTokens
	}
	if delta.TotalTokens < 0 {
		delta.TotalTokens = current.TotalTokens
	}
	if delta.PromptCacheHitTokens < 0 {
		delta.PromptCacheHitTokens = current.PromptCacheHitTokens
	}
	if delta.PromptCacheMissTokens < 0 {
		delta.PromptCacheMissTokens = current.PromptCacheMissTokens
	}
	if delta.ReasoningTokens < 0 {
		delta.ReasoningTokens = current.ReasoningTokens
	}
	if delta.Cost < 0 {
		delta.Cost = current.Cost
	}
	return delta
}

func (c *TurnCoordinator) finishModelAttemptLocked(event EventType, status ModelAttemptStatus, command TurnCommand) error {
	if len(c.attempts) > 0 {
		last := &c.attempts[len(c.attempts)-1]
		last.Status = status
		last.ErrorKind = command.ErrorKind
		finished := command.At
		last.FinishedAt = &finished
	}
	return c.step.Advance(event, command.At, command.Reason)
}

func (c *TurnCoordinator) retryModelAttemptLocked(command TurnCommand) error {
	if len(c.attempts) > 0 {
		last := &c.attempts[len(c.attempts)-1]
		last.Status = ModelAttemptStatusFailed
		last.ErrorKind = command.ErrorKind
		finished := command.At
		last.FinishedAt = &finished
	}
	return c.step.Advance(EventModelRequestRetrying, command.At, command.Reason)
}

func (c *TurnCoordinator) recordToolCallLocked(command TurnCommand) error {
	if c.step == nil || c.batch == nil {
		return fmt.Errorf("tool call requires an active tool batch")
	}
	callID := strings.TrimSpace(command.ToolCallID)
	if callID == "" {
		return fmt.Errorf("tool call id is required")
	}
	for _, existing := range c.batch.ToolCallIDs {
		if existing == callID {
			for _, execution := range c.executions {
				if execution.ToolCallID != callID {
					continue
				}
				if execution.ToolName != command.ToolName || string(execution.Arguments) != string(command.Arguments) {
					return fmt.Errorf("duplicate tool call %s has different identity", callID)
				}
				return nil
			}
			return nil
		}
	}
	c.batch.ToolCallIDs = append(c.batch.ToolCallIDs, callID)
	c.turn.Usage.ToolCalls++
	executionID := strings.TrimSpace(command.ToolExecutionID)
	if executionID == "" {
		executionID = callID + "-execution"
	}
	c.executions[executionID] = &ToolExecution{
		ID: executionID, ToolBatchID: c.batch.ID, ToolCallID: callID,
		ToolName: command.ToolName, Arguments: append(json.RawMessage(nil), command.Arguments...),
		Status: ToolExecutionStatusProposed, Attempt: 0,
	}
	return nil
}

func (c *TurnCoordinator) advanceToolExecutionLocked(command TurnCommand) error {
	if c.step == nil || c.batch == nil {
		return fmt.Errorf("tool execution requires an active tool batch")
	}
	executionID := strings.TrimSpace(command.ToolExecutionID)
	if executionID == "" {
		return fmt.Errorf("tool execution id is required")
	}
	execution := c.executions[executionID]
	if execution == nil {
		return fmt.Errorf("unknown tool execution %s", executionID)
	}
	switch command.Type {
	case CommandToolExecutionStarted:
		if execution.Status == ToolExecutionStatusRunning {
			return nil
		}
		if execution.Status != ToolExecutionStatusProposed {
			return fmt.Errorf("tool execution %s cannot start from %s", execution.ID, execution.Status)
		}
		execution.Attempt++
		if execution.Attempt == 0 {
			execution.Attempt = 1
		}
		execution.Status = ToolExecutionStatusRunning
		execution.StartedAt = command.At
	case CommandToolExecutionRetrying:
		if execution.Status != ToolExecutionStatusRunning && execution.Status != ToolExecutionStatusFailed {
			return fmt.Errorf("tool execution %s cannot retry from %s", execution.ID, execution.Status)
		}
		execution.Attempt++
		c.turn.Usage.ToolRetries++
		execution.Status = ToolExecutionStatusRunning
		execution.ErrorKind = command.ErrorKind
		execution.StartedAt = command.At
	case CommandToolExecutionCompleted:
		if execution.Status == ToolExecutionStatusSucceeded {
			return nil
		}
		if execution.Status != ToolExecutionStatusRunning {
			return fmt.Errorf("tool execution %s cannot complete from %s", execution.ID, execution.Status)
		}
		execution.Status = ToolExecutionStatusSucceeded
		execution.ResultRef = command.PayloadRef
		finished := command.At
		execution.FinishedAt = &finished
	case CommandToolExecutionFailed:
		status := command.ExecutionStatus
		if !status.Terminal() {
			status = ToolExecutionStatusFailed
		}
		if status == ToolExecutionStatusUnknown {
			if execution.Status == ToolExecutionStatusUnknown {
				return nil
			}
			if execution.Status.Terminal() {
				return fmt.Errorf("tool execution %s cannot become unknown from %s", execution.ID, execution.Status)
			}
		} else if execution.Status != ToolExecutionStatusRunning && execution.Status != ToolExecutionStatusProposed {
			if execution.Status == status {
				return nil
			}
			return fmt.Errorf("tool execution %s cannot fail from %s", execution.ID, execution.Status)
		}
		execution.Status = status
		execution.ErrorKind = command.ErrorKind
		if status == ToolExecutionStatusUnknown {
			c.recoveryRequired = true
		}
		finished := command.At
		execution.FinishedAt = &finished
	case CommandToolExecutionReconciled:
		if execution.Status != ToolExecutionStatusUnknown {
			return fmt.Errorf("tool execution %s is not unknown", execution.ID)
		}
		status := command.ExecutionStatus
		if !status.Terminal() || status == ToolExecutionStatusUnknown {
			return fmt.Errorf("reconciled tool execution %s requires a known terminal status", execution.ID)
		}
		execution.Status = status
		execution.ErrorKind = command.ErrorKind
		execution.ResultRef = command.PayloadRef
		finished := command.At
		execution.FinishedAt = &finished
		c.recoveryRequired = c.hasUnknownExecutionLocked()
	}
	return nil
}

// recordExternalFactLocked records a result that arrived from outside the
// active tool execution boundary (for example an async job completion or an
// external trigger). It deliberately does not create a ToolExecution: the
// fact is evidence for the current Turn, not a new model-requested tool.
func (c *TurnCoordinator) recordExternalFactLocked(command TurnCommand) error {
	if c.turn == nil || c.step == nil {
		return fmt.Errorf("external fact requires an active step")
	}
	factID := strings.TrimSpace(command.ExternalFactID)
	if factID == "" {
		return fmt.Errorf("external fact id is required")
	}
	if c.externalFacts == nil {
		c.externalFacts = make(map[string]bool)
	}
	if c.externalFacts[factID] {
		return nil
	}
	c.externalFacts[factID] = true
	return nil
}

func (c *TurnCoordinator) hasUnknownExecutionLocked() bool {
	for _, execution := range c.executions {
		if execution.Status == ToolExecutionStatusUnknown {
			return true
		}
	}
	return false
}

func (c *TurnCoordinator) interactionRequestedLocked(command TurnCommand) error {
	if c.turn == nil || c.step == nil {
		return fmt.Errorf("interaction request requires an active step")
	}
	// A legacy event stream may already have moved the Step to waiting before
	// the serialized PendingHITL payload was introduced. Treat the follow-up
	// request as an idempotent payload completion instead of attempting an
	// impossible waiting→waiting state transition.
	if c.step.Status == StepStatusWaitingInteraction && c.interaction != nil {
		if command.InteractionID != "" && command.InteractionID != c.interaction.ID {
			return fmt.Errorf("interaction mismatch: got=%s want=%s", command.InteractionID, c.interaction.ID)
		}
		if command.InteractionKind != "" {
			c.interaction.Kind = command.InteractionKind
		}
		if len(command.Payload) > 0 {
			c.interaction.Payload = append(json.RawMessage(nil), command.Payload...)
		}
		if command.ToolExecutionID != "" {
			c.interaction.ToolExecutionID = command.ToolExecutionID
		}
		return nil
	}
	if err := c.turn.Advance(EventInteractionRequested, command.At, command.Reason); err != nil {
		return err
	}
	interactionID := strings.TrimSpace(command.InteractionID)
	if interactionID == "" {
		interactionID = c.step.ID + "-interaction"
	}
	c.interaction = &PendingInteraction{
		ID: interactionID, TurnID: c.turn.ID, StepID: c.step.ID,
		Kind: command.InteractionKind, Status: InteractionStatusPending,
		ToolExecutionID: command.ToolExecutionID,
		Payload:         append(json.RawMessage(nil), command.Payload...), Revision: 1,
	}
	return c.step.Advance(EventInteractionRequested, command.At, command.Reason)
}

func (c *TurnCoordinator) interactionResolvedLocked(command TurnCommand) error {
	if c.turn == nil || c.step == nil {
		return fmt.Errorf("interaction resolution requires an active step")
	}
	if err := c.turn.Advance(EventInteractionResolved, command.At, command.Reason); err != nil {
		return err
	}
	if c.interaction != nil {
		if command.InteractionID != "" && command.InteractionID != c.interaction.ID {
			return fmt.Errorf("interaction mismatch: got=%s want=%s", command.InteractionID, c.interaction.ID)
		}
		if command.InteractionRevision != 0 && command.InteractionRevision != c.interaction.Revision {
			return fmt.Errorf("interaction revision mismatch: got=%d want=%d", command.InteractionRevision, c.interaction.Revision)
		}
		c.interaction.Status = InteractionStatusResolved
		c.interaction.Resolution = append(json.RawMessage(nil), command.Payload...)
		c.interaction.Revision++
	}
	return c.step.Advance(EventInteractionResolved, command.At, command.Reason)
}

func (c *TurnCoordinator) advanceStepLocked(event EventType, command TurnCommand) error {
	if c.step == nil {
		return fmt.Errorf("step command requires an active step")
	}
	if event == EventToolBatchCreated {
		if c.batch != nil {
			// AssistantReceived creates the batch as part of the same durable
			// fact. An explicit batch event may be replayed as a separate fact;
			// do not advance the Step twice.
			return nil
		}
		batchID := strings.TrimSpace(command.ToolBatchID)
		if batchID == "" {
			batchID = c.step.ID + "-batch"
		}
		c.batch = &ToolBatch{ID: batchID, StepID: c.step.ID, Status: "created"}
		c.step.ToolBatchID = batchID
	}
	if event == EventToolBatchSettled && c.batch != nil {
		for _, execution := range c.executions {
			if !execution.Status.Terminal() {
				return fmt.Errorf("tool batch has unfinished execution %s", execution.ID)
			}
			if execution.Status == ToolExecutionStatusUnknown {
				return fmt.Errorf("tool batch has unreconciled execution %s", execution.ID)
			}
		}
		c.batch.Status = "settled"
	}
	if event == EventStepCompleted && c.batch != nil && c.batch.Status != "settled" {
		return fmt.Errorf("cannot complete step before tool batch is settled")
	}
	return c.step.Advance(event, command.At, command.Reason)
}

func (c *TurnCoordinator) advanceTurnLocked(event EventType, command TurnCommand) error {
	if c.turn == nil {
		return fmt.Errorf("turn command requires an active turn")
	}
	return c.turn.Advance(event, command.At, command.Reason)
}

func (c *TurnCoordinator) contextCompactedLocked(command TurnCommand) error {
	if c.turn == nil || c.step == nil {
		return fmt.Errorf("context compaction requires an active step")
	}
	c.turn.ContextEpoch++
	c.step.ContextEpoch = c.turn.ContextEpoch
	return c.step.Advance(EventContextCompacted, command.At, command.Reason)
}

func (c *TurnCoordinator) snapshotLocked() CoordinatorSnapshot {
	result := CoordinatorSnapshot{
		SessionID:     c.sessionID,
		Generation:    c.generation,
		HasActiveTurn: c.turn != nil && !c.turn.Status.Terminal(),
	}
	if c.turn != nil {
		result.TurnID = c.turn.ID
		result.TurnStatus = c.turn.Status
		result.TurnEndReason = c.turn.EndReason
		result.StepIndex = c.turn.StepIndex
		result.ContextEpoch = c.turn.ContextEpoch
		result.RuntimeRevision = c.turn.RuntimeRevision
		result.Budget = c.turn.Budget
		result.Usage = c.turn.Usage
		result.RecoveryRequired = c.recoveryRequired
		for factID, recorded := range c.externalFacts {
			if strings.TrimSpace(factID) != "" && recorded {
				result.ExternalFacts++
			}
		}
		if c.turn.ContextSnapshot != nil {
			result.ContextSnapshot = c.turn.ContextSnapshot.Clone()
			result.RuntimeDigest = c.turn.ContextSnapshot.RuntimeDigest
			result.PromptDigest = c.turn.ContextSnapshot.PromptDigest
			result.ToolDigest = c.turn.ContextSnapshot.ToolDigest
		}
	}
	if c.step != nil {
		result.StepID = c.step.ID
		result.StepStatus = c.step.Status
		result.StepEndReason = c.step.EndReason
		result.ToolBatchID = c.step.ToolBatchID
		result.FinalSummary = c.step.FinalSummary
		result.AssistantMsgID = c.step.AssistantMsgID
	}
	if len(c.executions) > 0 {
		result.ToolExecutions = make([]ToolExecutionView, 0, len(c.executions))
		for _, execution := range c.executions {
			if execution == nil {
				continue
			}
			result.ToolExecutions = append(result.ToolExecutions, ToolExecutionView{
				ID: execution.ID, ToolCallID: execution.ToolCallID, ToolName: execution.ToolName,
				Status: execution.Status, Attempt: execution.Attempt,
			})
		}
		sort.Slice(result.ToolExecutions, func(i, j int) bool {
			return result.ToolExecutions[i].ID < result.ToolExecutions[j].ID
		})
	}
	if c.interaction != nil {
		result.InteractionID = c.interaction.ID
		result.InteractionKind = c.interaction.Kind
		result.InteractionPayload = append(json.RawMessage(nil), c.interaction.Payload...)
	}
	if len(c.attempts) > 0 {
		result.ModelAttempt = c.attempts[len(c.attempts)-1].Attempt
	}
	if c.step != nil && c.step.Status == StepStatusExecutingTools {
		if c.recoveryRequired {
			result.RecoveryRequired = true
		}
		for _, execution := range c.executions {
			if !execution.Status.Terminal() {
				result.RecoveryRequired = true
				break
			}
		}
	}
	return result
}

// BudgetDecisionFor is the side-effect preflight used by the runtime and
// orchestrator immediately before opening a new model/tool execution edge.
// Accounting is updated only by accepted lifecycle facts, so duplicate
// observer callbacks cannot consume budget twice.
func (c *TurnCoordinator) BudgetDecisionFor(command CommandType) BudgetDecision {
	return c.BudgetDecisionForCommand(TurnCommand{Type: command})
}

// BudgetDecisionForCommand is the contextual form used by the final summary
// path. Ordinary callers can keep using BudgetDecisionFor; a summary is a
// separate, explicitly reserved Step and never silently bypasses a budget.
func (c *TurnCoordinator) BudgetDecisionForCommand(command TurnCommand) BudgetDecision {
	if c == nil {
		return BudgetDecision{Allowed: false, Reason: "turn coordinator is nil"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	decision := BudgetDecision{Allowed: true}
	if c.turn == nil {
		return decision
	}
	decision.Budget = c.turn.Budget
	decision.Usage = c.turn.Usage
	if c.turn.Budget.MaxWallTime > 0 && !c.turn.StartedAt.IsZero() && time.Since(c.turn.StartedAt) >= c.turn.Budget.MaxWallTime {
		decision.Allowed = false
		decision.Reason = "max_wall_time"
		return decision
	}
	if c.turn.Budget.MaxInputTokens > 0 && c.turn.Usage.InputTokens >= c.turn.Budget.MaxInputTokens {
		decision.Allowed = false
		decision.Reason = "max_input_tokens"
		return decision
	}
	if c.turn.Budget.MaxOutputTokens > 0 && c.turn.Usage.OutputTokens >= c.turn.Budget.MaxOutputTokens {
		decision.Allowed = false
		decision.Reason = "max_output_tokens"
		return decision
	}
	if c.turn.Budget.MaxCost > 0 && c.turn.Usage.Cost >= c.turn.Budget.MaxCost {
		decision.Allowed = false
		decision.Reason = "max_cost"
		return decision
	}
	switch command.Type {
	case CommandStartStep:
		if command.FinalSummary {
			if !c.turn.Budget.ReserveFinalSummary || c.turn.Usage.SummarySteps >= 1 {
				decision.Allowed = false
				decision.Reason = "max_summary_steps"
			}
		} else if c.turn.Budget.MaxSteps > 0 && c.turn.Usage.Steps >= c.turn.Budget.MaxSteps {
			decision.Allowed = false
			decision.Reason = "max_steps"
		} else if c.turn.Budget.MaxToolCalls > 0 && c.turn.Usage.ToolCalls >= c.turn.Budget.MaxToolCalls {
			decision.Allowed = false
			decision.Reason = "max_tool_calls"
		}
	case CommandToolCallRecorded:
		// ToolCall facts are emitted before this preflight reaches the actual
		// execution edge, so exactly MaxToolCalls is still an allowed batch.
		if c.turn.Budget.MaxToolCalls > 0 && c.turn.Usage.ToolCalls > c.turn.Budget.MaxToolCalls {
			decision.Allowed = false
			decision.Reason = "max_tool_calls"
		}
	case CommandToolExecutionRetrying:
		if c.turn.Budget.MaxToolRetries > 0 && c.turn.Usage.ToolRetries >= c.turn.Budget.MaxToolRetries {
			decision.Allowed = false
			decision.Reason = "max_tool_retries"
		}
	}
	return decision
}

func (c *TurnCoordinator) Snapshot() CoordinatorSnapshot {
	if c == nil {
		return CoordinatorSnapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

// HasToolCall reports whether the active ToolBatch already contains a call.
// It lets compatibility adapters remain idempotent when a resume/recovery
// path observes the same assistant message more than once.
func (c *TurnCoordinator) HasToolCall(toolCallID string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.batch == nil {
		return false
	}
	for _, id := range c.batch.ToolCallIDs {
		if id == toolCallID {
			return true
		}
	}
	return false
}

// ToolExecutionID returns the execution record for a ToolCall in the active
// batch. It is intentionally read-only and returns an empty string when the
// call came from a legacy projection without an execution record.
func (c *TurnCoordinator) ToolExecutionID(toolCallID string) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, execution := range c.executions {
		if execution.ToolCallID == toolCallID {
			return id
		}
	}
	return ""
}

// ToolExecutionStatusForCall exposes the projected execution phase to the
// compatibility adapter so an execution-start fact is not emitted twice when
// the real tool boundary and the history reconciliation both observe it.
func (c *TurnCoordinator) ToolExecutionStatusForCall(toolCallID string) (ToolExecutionStatus, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, execution := range c.executions {
		if execution.ToolCallID == toolCallID {
			return execution.Status, true
		}
	}
	return "", false
}

// ToolExecutionInfo returns the stable model-facing identity needed by a
// reconciliation adapter to append one deterministic ToolResult projection.
func (c *TurnCoordinator) ToolExecutionInfo(executionID string) (toolCallID, toolName string, ok bool) {
	if c == nil {
		return "", "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	execution := c.executions[executionID]
	if execution == nil {
		return "", "", false
	}
	return execution.ToolCallID, execution.ToolName, true
}

// HasToolResult reports whether the durable projection already accepted the
// final result for a ToolCall. It keeps history reconciliation idempotent when
// a process dies between writing the model message and the lifecycle event.
func (c *TurnCoordinator) HasToolResult(toolCallID string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.toolResults[strings.TrimSpace(toolCallID)]
}

// InFlightToolExecutionIDs returns executions that have no durable terminal
// result. The order follows the original ToolCall order so recovery decisions
// and diagnostics remain deterministic even when execution was concurrent.
func (c *TurnCoordinator) InFlightToolExecutionIDs() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.batch == nil {
		return nil
	}
	byCall := make(map[string]string, len(c.executions))
	for id, execution := range c.executions {
		byCall[execution.ToolCallID] = id
	}
	result := make([]string, 0)
	for _, callID := range c.batch.ToolCallIDs {
		id := byCall[callID]
		if execution := c.executions[id]; execution != nil && !execution.Status.Terminal() {
			result = append(result, id)
		}
	}
	if len(result) == 0 {
		return nil
	}
	// A malformed legacy projection may not have a corresponding ToolCall;
	// keep its IDs visible rather than silently losing recovery information.
	seen := make(map[string]struct{}, len(result))
	for _, id := range result {
		seen[id] = struct{}{}
	}
	for id, execution := range c.executions {
		if !execution.Status.Terminal() {
			if _, ok := seen[id]; !ok {
				result = append(result, id)
			}
		}
	}
	return result
}
