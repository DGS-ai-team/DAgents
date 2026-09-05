package turn

import (
	"context"
	"strings"
)

type executionContextKey struct{}

// TurnExecutionContext is the immutable identity of one model/tool Step.
//
// The SessionRuntime owns execution resources such as the cancel function and
// message history, while TurnCoordinator owns this logical identity. Queue
// continuations and provider callbacks carry this value instead of copying a
// second turn/generation state machine into the runtime.
type TurnExecutionContext struct {
	SessionID    string
	TurnID       string
	StepID       string
	Generation   uint64
	StepIndex    int
	ContextEpoch int
}

func (c TurnExecutionContext) Valid() bool {
	return strings.TrimSpace(c.SessionID) != "" &&
		strings.TrimSpace(c.TurnID) != "" &&
		strings.TrimSpace(c.StepID) != "" &&
		c.Generation > 0
}

func (c TurnExecutionContext) SameStep(other TurnExecutionContext) bool {
	return c.SessionID == other.SessionID &&
		c.TurnID == other.TurnID &&
		c.StepID == other.StepID &&
		c.Generation == other.Generation
}

func WithExecutionContext(ctx context.Context, execution TurnExecutionContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, executionContextKey{}, execution)
}

func ExecutionContextFromContext(ctx context.Context) (TurnExecutionContext, bool) {
	if ctx == nil {
		return TurnExecutionContext{}, false
	}
	execution, ok := ctx.Value(executionContextKey{}).(TurnExecutionContext)
	return execution, ok
}

// StepIndexFromContext returns the logical Step number carried by the active
// execution context. Direct Orchestrator unit callers that do not install a
// runtime context use Step 1 as a safe direct-call default; production
// session execution always supplies the Coordinator-owned index.
func StepIndexFromContext(ctx context.Context) int {
	if execution, ok := ExecutionContextFromContext(ctx); ok && execution.StepIndex > 0 {
		return execution.StepIndex
	}
	return 1
}

func (s CoordinatorSnapshot) ExecutionContext() TurnExecutionContext {
	return TurnExecutionContext{
		SessionID:    s.SessionID,
		TurnID:       s.TurnID,
		StepID:       s.StepID,
		Generation:   s.Generation,
		StepIndex:    s.StepIndex,
		ContextEpoch: s.ContextEpoch,
	}
}

// ExecutionContext returns the current active Step identity. A terminal or
// partially restored projection may return an invalid context; callers must
// check Valid before opening model/tool side effects.
func (c *TurnCoordinator) ExecutionContext() TurnExecutionContext {
	if c == nil {
		return TurnExecutionContext{}
	}
	return c.Snapshot().ExecutionContext()
}

// IsCurrentExecution checks that a callback still belongs to the active Step.
// It deliberately requires an active Turn and Step so a late callback cannot
// create a new continuation after cancellation or terminal completion.
func (c *TurnCoordinator) IsCurrentExecution(ctx TurnExecutionContext) bool {
	if c == nil || !ctx.Valid() {
		return false
	}
	state := c.Snapshot()
	return state.HasActiveTurn && !state.StepStatus.Terminal() &&
		state.ExecutionContext().SameStep(ctx)
}
