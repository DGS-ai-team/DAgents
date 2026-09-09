package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// DreamingTurnResult is the successful model output that may be committed by
// the autonomy store. Session deliberately does not persist experience or
// context-reset state here.
type DreamingTurnResult struct {
	Content    string
	Changed    bool
	Usage      turn.TurnUsage
	UsageKnown bool
	TurnID     string
	Boundary   string
}

// DreamingMetadata is supplied by the trusted scheduler. The variadic form
// keeps embedded callers source-compatible while production callers can bind
// the attempt to the occurrence and experience revision.
type DreamingMetadata struct {
	LocalDate          string
	ExperienceRevision int64
}

// GetDreamingAttempt returns the persisted attempt projection. It is a
// read-only copy so schedulers cannot manufacture completion for a session.
func (m *Manager) GetDreamingAttempt(sessionID string) (DreamingAttempt, bool, error) {
	if m == nil || strings.TrimSpace(sessionID) == "" {
		return DreamingAttempt{}, false, fmt.Errorf("session id is required")
	}
	r := m.getRuntime(strings.TrimSpace(sessionID))
	if r == nil {
		return DreamingAttempt{}, false, fmt.Errorf("agent_not_found")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dreamingAttempt == nil {
		return DreamingAttempt{}, false, nil
	}
	return *r.dreamingAttempt, true, nil
}

// AckDreamingAttempt removes only a completed attempt after the caller has
// durably reset the active context. A waiting/failed attempt remains available
// for recovery and diagnosis.
func (m *Manager) AckDreamingAttempt(ctx context.Context, sessionID, turnID string) error {
	r := m.getRuntime(strings.TrimSpace(sessionID))
	if r == nil || strings.TrimSpace(turnID) == "" {
		return fmt.Errorf("dreaming attempt not found")
	}
	if ctx == nil || r.executionGate == nil || !r.executionGate.owns(ctx) {
		return fmt.Errorf("maintenance lease required")
	}
	r.mu.Lock()
	a := r.dreamingAttempt
	if a == nil || a.TurnID != strings.TrimSpace(turnID) || a.State != DreamingAttemptCompleted {
		r.mu.Unlock()
		return fmt.Errorf("dreaming attempt is not completed")
	}
	if strings.TrimSpace(r.lastContextResetID) == "" {
		r.mu.Unlock()
		return fmt.Errorf("dreaming context reset is not durable")
	}
	b, err := decodeActiveContextBoundary(a.Boundary, r.session.ID)
	if err != nil || b.Index != r.activeContextStart || activeContextPrefixDigest(r.messages, b.Index) != b.Prefix {
		r.mu.Unlock()
		return fmt.Errorf("dreaming context reset does not match attempt boundary")
	}
	r.dreamingAttempt = nil
	r.mu.Unlock()
	if err := r.persist(ctx); err != nil {
		r.mu.Lock()
		r.dreamingAttempt = a
		r.mu.Unlock()
		return err
	}
	return nil
}

func (r *runtime) saveDreamingAttempt(attempt *DreamingAttempt) error {
	if attempt == nil {
		return fmt.Errorf("dreaming attempt is required")
	}
	if err := attempt.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	old := r.dreamingAttempt
	candidate := *attempt
	r.dreamingAttempt = &candidate
	r.mu.Unlock()
	if err := r.persist(context.Background()); err != nil {
		r.mu.Lock()
		r.dreamingAttempt = old
		r.mu.Unlock()
		return err
	}
	return nil
}

// completeDreamingResume records the result produced by the resumed turn
// only. The continuation history is the lifecycle operation's returned batch;
// it is intentionally not selected from the session's latest messages.
func (r *runtime) completeDreamingResume(history []llm.Message, outcome turn.StepOutcome) error {
	r.mu.Lock()
	if r.dreamingAttempt == nil {
		r.mu.Unlock()
		return nil
	}
	attemptValue := *r.dreamingAttempt
	r.mu.Unlock()
	attempt := &attemptValue
	if attempt.State != DreamingAttemptWaiting && attempt.State != DreamingAttemptRunning {
		return nil
	}
	state := r.turnCoordinator.Snapshot()
	if outcome.Pending != nil || state.StepStatus == turn.StepStatusWaitingInteraction {
		attempt.State = DreamingAttemptWaiting
		return r.saveDreamingAttempt(attempt)
	}
	if outcome.Err != nil || state.TurnStatus != turn.TurnStatusCompleted || state.StepStatus != turn.StepStatusCompleted {
		terminal := DreamingAttemptFailed
		if errors.Is(outcome.Err, context.Canceled) || state.TurnStatus == turn.TurnStatusCancelled {
			terminal = DreamingAttemptCancelled
		}
		updated, err := attempt.Fail(terminal, time.Now().UTC())
		if err != nil {
			return err
		}
		return r.saveDreamingAttempt(&updated)
	}
	for _, execution := range state.ToolExecutions {
		if execution.Status != turn.ToolExecutionStatusSucceeded {
			updated, err := attempt.Fail(DreamingAttemptFailed, time.Now().UTC())
			if err != nil {
				return err
			}
			return r.saveDreamingAttempt(&updated)
		}
	}
	if r.orch != nil && r.orch.ToolRegistry() != nil && r.orch.ToolRegistry().HandbookMutationCount() <= attempt.HandbookMutationBefore {
		for _, message := range r.messagesForDreaming(*attempt) {
			if len(message.ToolCalls) > 0 {
				updated, err := attempt.Fail(DreamingAttemptFailed, time.Now().UTC())
				if err != nil {
					return err
				}
				return r.saveDreamingAttempt(&updated)
			}
		}
	}
	final, ok := r.dreamingFinalMessage(*attempt)
	if !ok || len(final.ToolCalls) != 0 || strings.TrimSpace(final.Content) == "" {
		return fmt.Errorf("dreaming resume result is empty")
	}
	boundary, err := r.captureActiveContextBoundary()
	if err != nil {
		return err
	}
	updated, err := attempt.Complete(state.TurnID, state.AssistantMsgID, final.Content, boundary, state.Usage, state.ModelUsageKnown, time.Now().UTC())
	if err != nil {
		return err
	}
	return r.saveDreamingAttempt(&updated)
}

// recoverDreamingAttemptAfterLifecycle fences an interrupted attempt whose
// turn is no longer recoverable. It deliberately records failure rather than
// guessing completion or replaying tools.
func (r *runtime) recoverDreamingAttemptAfterLifecycle() (bool, error) {
	r.mu.Lock()
	if r.dreamingAttempt == nil || (r.dreamingAttempt.State != DreamingAttemptRunning && r.dreamingAttempt.State != DreamingAttemptWaiting) {
		r.mu.Unlock()
		return false, nil
	}
	attempt := *r.dreamingAttempt
	r.mu.Unlock()
	state := r.turnCoordinator.Snapshot()
	if state.TurnID == attempt.TurnID && state.HasActiveTurn && !state.TurnStatus.Terminal() {
		return false, nil
	}
	updated, err := attempt.Fail(DreamingAttemptFailed, time.Now().UTC())
	if err != nil {
		return false, err
	}
	if err := r.saveDreamingAttempt(&updated); err != nil {
		return false, err
	}
	return true, nil
}

func (r *runtime) markDreamingCancelled(turnID string) error {
	r.mu.Lock()
	if r.dreamingAttempt == nil || r.dreamingAttempt.TurnID != strings.TrimSpace(turnID) || (r.dreamingAttempt.State != DreamingAttemptRunning && r.dreamingAttempt.State != DreamingAttemptWaiting) {
		r.mu.Unlock()
		return nil
	}
	attempt := *r.dreamingAttempt
	r.mu.Unlock()
	updated, err := attempt.Fail(DreamingAttemptCancelled, time.Now().UTC())
	if err != nil {
		return err
	}
	return r.saveDreamingAttempt(&updated)
}

func (r *runtime) captureActiveContextBoundary() (string, error) {
	r.mu.Lock()
	b, err := json.Marshal(activeContextBoundary{SessionID: r.session.ID, Revision: r.historyRevision, Index: len(r.messages), Prefix: activeContextPrefixDigest(r.messages, len(r.messages))})
	r.mu.Unlock()
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (r *runtime) dreamingFinalMessage(attempt DreamingAttempt) (llm.Message, bool) {
	messages := r.messagesForDreaming(attempt)
	return lastAssistantMessage(messages, 0)
}

func (r *runtime) messagesForDreaming(attempt DreamingAttempt) []llm.Message {
	r.mu.Lock()
	start := attempt.HistoryStart
	if start < 0 || start > len(r.messages) {
		r.mu.Unlock()
		return nil
	}
	messages := append([]llm.Message(nil), r.messages[start:]...)
	r.mu.Unlock()
	return messages
}

// RunDreaming runs on an already-created main session while its maintenance
// lease is held. It uses the real Agent model and handbook tools, but has no
// receipt, token-budget, or external-fact side channel.
func (m *Manager) RunDreaming(ctx context.Context, sessionID, prompt string, maxToolRounds int, metadata ...DreamingMetadata) (DreamingTurnResult, error) {
	if m == nil || ctx == nil || maxToolRounds <= 0 {
		return DreamingTurnResult{}, fmt.Errorf("dreaming executor unavailable")
	}
	m.mu.RLock()
	r := m.sessions[sessionID]
	var gate *agentExecutionGate
	if r != nil {
		gate = m.maintenanceGates[r.agentID]
	}
	m.mu.RUnlock()
	if r == nil || r.orch == nil || !r.autoAgent {
		return DreamingTurnResult{}, fmt.Errorf("dreaming requires an Auto session")
	}
	if gate == nil || !gate.owns(ctx) {
		return DreamingTurnResult{}, fmt.Errorf("maintenance gate is not held")
	}
	r.mu.Lock()
	if r.dreamingAttempt != nil && r.dreamingAttempt.State != DreamingAttemptFailed && r.dreamingAttempt.State != DreamingAttemptCancelled {
		id := r.dreamingAttempt.TurnID
		r.mu.Unlock()
		return DreamingTurnResult{TurnID: id}, fmt.Errorf("dreaming turn is already active")
	}
	r.mu.Unlock()
	reg := r.orch.ToolRegistry()
	if reg == nil || strings.TrimSpace(reg.HandbookRoot()) == "" {
		return DreamingTurnResult{}, fmt.Errorf("handbook is unavailable")
	}
	boundary, err := m.CaptureActiveContextBoundary(ctx, sessionID)
	if err != nil {
		return DreamingTurnResult{}, err
	}
	mutationBefore := reg.HandbookMutationCount()
	dreamBudget := turn.TurnBudget{MaxToolRounds: maxToolRounds, ReserveFinalSummary: true}
	r.lifecycleMu.Lock()
	previousBudget, previousResolver := r.turnBudget, r.budgetResolver
	// A configured resolver normally supplies the user-chat budget. Dreaming
	// has its own bounded tool-round budget, so suspend that resolver only for
	// this lifecycle start and restore it before returning.
	r.turnBudget = dreamBudget
	r.budgetResolver = func() (turn.TurnBudget, error) { return dreamBudget, nil }
	r.lifecycleMu.Unlock()
	defer func() {
		r.lifecycleMu.Lock()
		r.turnBudget, r.budgetResolver = previousBudget, previousResolver
		r.lifecycleMu.Unlock()
	}()

	user := llm.UserMessage(strings.TrimSpace(prompt), llm.UserNameHuman)
	r.mu.Lock()
	r.pendingInputMessage = &user
	r.mu.Unlock()
	beginErr := r.lifecycleBeginInputTurn(turn.TurnSourceSideEffect)
	r.mu.Lock()
	r.pendingInputMessage = nil
	r.mu.Unlock()
	if beginErr != nil {
		return DreamingTurnResult{}, beginErr
	}
	state := r.turnCoordinator.Snapshot()
	boundaryValue, boundaryErr := decodeActiveContextBoundary(boundary, r.session.ID)
	if boundaryErr != nil {
		return DreamingTurnResult{TurnID: state.TurnID}, boundaryErr
	}
	meta := DreamingMetadata{}
	if len(metadata) > 0 {
		meta = metadata[0]
	}
	attempt := &DreamingAttempt{AgentID: r.agentID, SessionID: r.session.ID, TurnID: state.TurnID,
		ExperienceRevision: meta.ExperienceRevision, LocalDate: strings.TrimSpace(meta.LocalDate), MaxToolRounds: maxToolRounds, State: DreamingAttemptRunning,
		Boundary: boundary, HistoryStart: boundaryValue.Index, HandbookMutationBefore: mutationBefore, StartedAt: time.Now().UTC()}
	if err := r.saveDreamingAttempt(attempt); err != nil {
		return DreamingTurnResult{TurnID: state.TurnID, Boundary: boundary}, err
	}
	historyStart := r.lifecycleHistoryLength()
	dreamCtx := tools.WithHandbookMaintenance(ctx)
	outcome, history := r.runTurnStepWithSideEffects(dreamCtx, false, func(stepCtx context.Context, h *[]llm.Message) turn.StepOutcome {
		return r.orch.RunHumanMessageTurn(stepCtx, r.session.ID, h, user)
	})
	if err := r.lifecycleAfterModelStep(outcome, history, historyStart); err != nil && outcome.Err == nil {
		outcome.Err = err
	}
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(dreamCtx, 0, outcome)
	r.finishTurnIdle(outcome)
	state = r.turnCoordinator.Snapshot()
	result := DreamingTurnResult{Usage: state.Usage, UsageKnown: state.ModelUsageKnown, Changed: reg.HandbookMutationCount() > mutationBefore, TurnID: state.TurnID, Boundary: boundary}
	if outcome.Pending != nil || state.StepStatus == turn.StepStatusWaitingInteraction {
		attemptWaiting := *attempt
		attemptWaiting.State = DreamingAttemptWaiting
		if saveErr := r.saveDreamingAttempt(&attemptWaiting); saveErr != nil {
			return result, saveErr
		}
		return result, fmt.Errorf("dreaming turn requires interaction")
	}
	if outcome.Err != nil {
		failedState := DreamingAttemptFailed
		if errors.Is(outcome.Err, context.Canceled) {
			failedState = DreamingAttemptCancelled
		}
		if updated, ferr := attempt.Fail(failedState, time.Now().UTC()); ferr == nil {
			if saveErr := r.saveDreamingAttempt(&updated); saveErr != nil {
				return result, saveErr
			}
		} else if ferr != nil {
			return result, ferr
		}
		return result, outcome.Err
	}
	final, ok := r.dreamingFinalMessage(*attempt)
	if !ok || len(final.ToolCalls) != 0 || strings.TrimSpace(final.Content) == "" ||
		state.TurnStatus != turn.TurnStatusCompleted || state.StepStatus != turn.StepStatusCompleted || state.HasActiveTurn {
		return result, fmt.Errorf("dreaming result is empty")
	}
	completedBoundary, cerr := r.captureActiveContextBoundary()
	if cerr != nil {
		return result, cerr
	}
	completed, cerr := attempt.Complete(state.TurnID, state.AssistantMsgID, final.Content, completedBoundary, state.Usage, state.ModelUsageKnown, time.Now().UTC())
	if cerr != nil {
		return result, cerr
	}
	if cerr = r.saveDreamingAttempt(&completed); cerr != nil {
		return result, cerr
	}
	result.Content = strings.TrimSpace(final.Content)
	result.Boundary = completedBoundary
	return result, nil
}
