package session

import (
	"context"
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// runTurnStepAtEpoch is the same execution scaffold with an optional queue
// epoch fence. Human messages use the envelope epoch so clear-context cannot
// create a new fence for an already accepted, but now stale, handler.
func (r *runtime) runTurnStepAtEpoch(
	parent context.Context,
	compressBefore bool,
	expectedEpoch uint64,
	run func(ctx context.Context, history *[]llm.Message) turn.StepOutcome,
) (turn.StepOutcome, []llm.Message) {
	compressBeforeStep := compressBefore && r.compression != nil && r.compression.Enabled() && !r.isChildSession()
	var sidecarPrefix compression.SidecarPrefix
	if compressBeforeStep {
		// sidecarPrefix / RunTurnBeforeCompressPhase → composeSystemPrompt → getLoadedSkills 会抢 r.mu，须在持锁前执行。
		sidecarPrefix = r.sidecarPrefix()
		if r.orch != nil {
			r.mu.Lock()
			start := r.activeContextStart
			if start < 0 || start > len(r.messages) {
				start = len(r.messages)
			}
			active := append([]llm.Message(nil), r.messages[start:]...)
			r.mu.Unlock()
			beforeHookDigest := turn.Digest(active)
			skip := r.orch.RunTurnBeforeCompressPhase(parent, r.session.ID, &active, false)
			if turn.Digest(active) != beforeHookDigest {
				r.mu.Lock()
				currentStart := r.activeContextStart
				if currentStart < 0 || currentStart > len(r.messages) {
					currentStart = len(r.messages)
				}
				r.messages = append(append([]llm.Message(nil), r.messages[:currentStart]...), active...)
				r.historyRevision++
				r.mu.Unlock()
			}
			if skip {
				compressBeforeStep = false
			}
		}
	}
	contextCompacted := false
	contextBeforeDigest := ""
	contextAfterDigest := ""
	contextBeforeCount := 0
	contextAfterCount := 0
	r.mu.Lock()
	if expectedEpoch != 0 && expectedEpoch != r.sessionEpoch {
		start := r.activeContextStart
		if start < 0 || start > len(r.messages) {
			start = len(r.messages)
		}
		history := append([]llm.Message(nil), r.messages[start:]...)
		r.mu.Unlock()
		return turn.StepOutcome{Err: context.Canceled}, history
	}
	if compressBeforeStep {
		start := r.activeContextStart
		if start < 0 || start > len(r.messages) {
			start = len(r.messages)
			r.activeContextStart = start
		}
		active := append([]llm.Message(nil), r.messages[start:]...)
		contextBeforeDigest = turn.Digest(active)
		contextBeforeCount = len(active)
		if r.compression.MaybeHandle(parent, r.session.ID, r.agentID, r.hub, &active, sidecarPrefix) {
			contextCompacted = true
			r.messages = append(append([]llm.Message(nil), r.messages[:start]...), active...)
			r.historyRevision++
			contextAfterDigest = turn.Digest(active)
			contextAfterCount = len(active)
		}
	}
	execution := r.turnCoordinator.ExecutionContext()
	if !execution.Valid() {
		start := r.activeContextStart
		if start < 0 || start > len(r.messages) {
			start = len(r.messages)
		}
		history := append([]llm.Message(nil), r.messages[start:]...)
		r.mu.Unlock()
		return turn.StepOutcome{Err: fmt.Errorf("cannot execute step without an active Turn/Step")}, history
	}
	executionEpoch := r.sessionEpoch
	turnCtx, cancel := context.WithCancel(parent)
	goalID, runID := r.goalID, r.runID
	if goalID != "" && runID != "" {
		turnCtx = tools.WithGoalRun(turnCtx, goalID, runID)
	}
	cancelToken := &struct{}{}
	turnCtx = turn.WithExecutionContext(turnCtx, execution)
	r.turnCancel = cancel
	r.turnCancelToken = cancelToken
	r.turnEpoch = executionEpoch
	r.turnFenceActive = true
	start := r.activeContextStart
	if start < 0 || start > len(r.messages) {
		start = len(r.messages)
		r.activeContextStart = start
	}
	history := append([]llm.Message(nil), r.messages[start:]...)
	r.mu.Unlock()

	defer func() {
		cancel()
		r.mu.Lock()
		if r.turnCancelToken == cancelToken {
			r.turnCancel = nil
			r.turnCancelToken = nil
		}
		r.mu.Unlock()
	}()

	if contextCompacted {
		if err := r.lifecycleContextCompacted("context_compressed_before_step", contextBeforeDigest, contextAfterDigest, contextBeforeCount, contextAfterCount); err != nil {
			return turn.StepOutcome{Err: fmt.Errorf("context compaction lifecycle failed: %w", err)}, history
		}
		// Compression changes the model-visible history boundary. Invalidate the
		// active context segment so the next request rebuilds system prompt,
		// tools, request-only injections and skill metadata from the compacted
		// history instead of combining new messages with an old snapshot.
		r.scheduleModelContextRebuild("context_compression", "next_model_step")
	}

	outcome := run(turnCtx, &history)
	return outcome, history
}

func (r *runtime) finishTurnIdle(outcome turn.StepOutcome) {
	if outcome.Err != nil && r.isChildSession() && r.childMeta != nil && r.childMeta.childMgr != nil {
		r.childMeta.childMgr.OnChildFailed(r.session.ID, outcome.Err.Error(), r.stepIndexSnapshot())
		return
	}
	if outcome.ScheduleToolResult || outcome.Pending != nil {
		return
	}
	// The orchestrator may enqueue the next tool step directly and return an
	// otherwise empty outcome (for example after a child-agent tool finishes).
	// Keep the current turn identity alive until that continuation is consumed;
	// otherwise the queue consumer would discard the freshly enqueued result as
	// stale.
	state := r.turnCoordinator.Snapshot()
	if state.HasActiveTurn && !state.TurnStatus.Terminal() {
		return
	}
	// No-work is a successful terminal fact only after lifecycle persistence has
	// completed without error. A failed/cancelled turn must follow the ordinary
	// error path and must never emit a successful no_work notification.
	if outcome.Err == nil && outcome.NoWork && state.TurnStatus == turn.TurnStatusCompleted && r.orch != nil {
		r.orch.PublishNoWorkFinished(r.session.ID)
	}
	if r.orch != nil {
		r.orch.EndAutoIdleActivation(r.session.ID)
	}
	r.tryCompleteChildIfIdle()
}
