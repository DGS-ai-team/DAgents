package session

import (
	"context"
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
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
			skip := r.orch.RunTurnBeforeCompressPhase(parent, r.session.ID, &r.messages, false)
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
		history := append([]llm.Message(nil), r.messages...)
		r.mu.Unlock()
		return turn.StepOutcome{Err: context.Canceled}, history
	}
	if compressBeforeStep {
		contextBeforeDigest = turn.Digest(r.messages)
		contextBeforeCount = len(r.messages)
		if r.compression.MaybeHandle(parent, r.session.ID, r.agentID, r.hub, &r.messages, sidecarPrefix) {
			contextCompacted = true
			r.historyRevision++
			contextAfterDigest = turn.Digest(r.messages)
			contextAfterCount = len(r.messages)
			if r.orch != nil {
				r.orch.ReloadLongTermMemory(parent)
			}
		}
	}
	execution := r.turnCoordinator.ExecutionContext()
	if !execution.Valid() {
		r.mu.Unlock()
		return turn.StepOutcome{Err: fmt.Errorf("cannot execute step without an active Turn/Step")}, r.messages
	}
	executionEpoch := r.sessionEpoch
	turnCtx, cancel := context.WithCancel(parent)
	cancelToken := &struct{}{}
	turnCtx = turn.WithExecutionContext(turnCtx, execution)
	r.turnCancel = cancel
	r.turnCancelToken = cancelToken
	r.turnEpoch = executionEpoch
	r.turnFenceActive = true
	history := r.messages
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
	if outcome.ScheduleToolResult || outcome.Pending != nil {
		return
	}
	// The orchestrator may enqueue the next tool step directly and return an
	// otherwise empty outcome (for example after a child-agent tool finishes).
	// Keep the current turn identity alive until that continuation is consumed;
	// otherwise the queue consumer would discard the freshly enqueued result as
	// stale.
	if state := r.turnCoordinator.Snapshot(); state.HasActiveTurn && !state.TurnStatus.Terminal() {
		return
	}
	r.tryCompleteChildIfIdle()
}
