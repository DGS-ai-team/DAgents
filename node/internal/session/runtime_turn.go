package session

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// runTurnStep 执行单步 turn 的通用脚手架：可选步前压缩、turnCtx、状态回调、收尾 idle。
func (r *runtime) runTurnStep(
	parent context.Context,
	initialState turn.State,
	compressBefore bool,
	run func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome,
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
	r.mu.Lock()
	if compressBeforeStep {
		r.compression.MaybeHandle(parent, r.session.ID, r.agentID, r.hub, &r.messages, sidecarPrefix)
	}
	turnCtx, cancel := context.WithCancel(parent)
	r.turnCancel = cancel
	r.state = initialState
	history := r.messages
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.state = turn.StateIdle
		r.turnCancel = nil
		r.mu.Unlock()
	}()

	setState := func(s turn.State) {
		r.mu.Lock()
		r.state = s
		r.mu.Unlock()
	}

	outcome := run(turnCtx, &history, setState)
	return outcome, history
}

func (r *runtime) finishTurnIdle(outcome turn.StepOutcome) {
	if outcome.ScheduleToolResult {
		return
	}
	r.tryCompleteChildIfIdle()
}
