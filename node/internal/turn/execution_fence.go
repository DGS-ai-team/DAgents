package turn

import "context"

// ExecutionFence is the runtime-owned admission check for a model/tool
// side-effect boundary. The coordinator decides whether the immutable
// TurnExecutionContext is still current; the orchestrator only asks that
// question and never maintains a second cancellation state machine.
type ExecutionFence func(TurnExecutionContext) bool

// ToolExecutionStatusReader exposes the coordinator's authoritative result
// race decision to the tool commit path. A completed execution wins over a
// later Turn cancellation; a cancelled execution must not be overwritten by
// a late provider/process result.
type ToolExecutionStatusReader func(sessionID, toolCallID string) (ToolExecutionStatus, bool)

// SetExecutionFence attaches the runtime/coordinator admission check.
func (o *Orchestrator) SetExecutionFence(fence ExecutionFence) {
	if o == nil {
		return
	}
	o.executionFence = fence
}

// SetToolExecutionStatusReader attaches the runtime/coordinator execution
// projection used to resolve the completion-vs-cancellation race.
func (o *Orchestrator) SetToolExecutionStatusReader(reader ToolExecutionStatusReader) {
	if o == nil {
		return
	}
	o.toolExecutionStatus = reader
}

// executionBoundaryOpen is used at C0-C4. In production the valid execution
// context is checked by the coordinator fence. A direct Orchestrator caller
// without a runtime fence retains the existing context cancellation behavior
// for direct unit callers.
func (o *Orchestrator) executionBoundaryOpen(ctx context.Context) bool {
	if o == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if o.executionFence != nil {
		execution, ok := ExecutionContextFromContext(ctx)
		if ok {
			return o.executionFence(execution)
		}
	}
	return true
}

func (o *Orchestrator) toolCancellationWon(ctx context.Context, sessionID, toolCallID string) bool {
	if o != nil && o.toolExecutionStatus != nil {
		if status, ok := o.toolExecutionStatus(sessionID, toolCallID); ok {
			return status == ToolExecutionStatusCancelled
		}
	}
	return !o.executionBoundaryOpen(ctx)
}
