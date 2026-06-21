package hooks

// Phase 标识 Hook 执行锚点（见 docs/design/agent-hooks.md §4）。
type Phase string

const (
	PhaseMessageEnqueued    Phase = "message.enqueued"
	PhaseTurnBeforeCompress Phase = "turn.before_compress"
	PhaseTurnBeforeStep     Phase = "turn.before_step"
	PhasePromptBuild        Phase = "prompt.build"
	PhaseLLMBeforeCall      Phase = "llm.before_call"
	PhaseLLMAfterCall       Phase = "llm.after_call"
	PhaseToolBeforeEach     Phase = "tool.before_each"
	PhaseToolAfterEach      Phase = "tool.after_each"
	PhaseHITLBeforePause    Phase = "hitl.before_pause"
	PhaseHITLAfterResume    Phase = "hitl.after_resume"
	PhaseTurnDone           Phase = "turn.done"
	PhaseTurnError          Phase = "turn.error"
	PhaseTurnCancel         Phase = "turn.cancel"
	PhaseSessionLifecycle   Phase = "session.lifecycle"
)
