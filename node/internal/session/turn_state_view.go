package session

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// TurnStateView is the small, UI-facing projection of the durable Turn/Step
// coordinator.  The lifecycle journal remains the source of truth; this
// projection deliberately omits model content and tool arguments so it is
// safe to publish on the normal per-agent SSE stream.
type TurnStateView struct {
	Authority        string                   `json:"authority"`
	Phase            string                   `json:"phase"`
	TurnStatus       string                   `json:"turn_status,omitempty"`
	StepStatus       string                   `json:"step_status,omitempty"`
	TurnID           string                   `json:"turn_id,omitempty"`
	StepID           string                   `json:"step_id,omitempty"`
	StepIndex        int                      `json:"step_index,omitempty"`
	Generation       uint64                   `json:"generation,omitempty"`
	LifecycleSeq     uint64                   `json:"lifecycle_seq"`
	HistoryRevision  uint64                   `json:"history_revision,omitempty"`
	Terminal         bool                     `json:"terminal"`
	EndReason        string                   `json:"end_reason,omitempty"`
	InteractionKind  string                   `json:"interaction_kind,omitempty"`
	RecoveryRequired bool                     `json:"recovery_required,omitempty"`
	ToolExecutions   []turn.ToolExecutionView `json:"tool_executions,omitempty"`
}

func buildTurnStateView(snapshot turn.CoordinatorSnapshot, lifecycleSeq uint64) TurnStateView {
	endReason := strings.TrimSpace(string(snapshot.TurnEndReason))
	if endReason == "" {
		endReason = strings.TrimSpace(string(snapshot.StepEndReason))
	}
	return TurnStateView{
		Authority:        "turn_coordinator",
		Phase:            turnStatePhase(snapshot),
		TurnStatus:       string(snapshot.TurnStatus),
		StepStatus:       string(snapshot.StepStatus),
		TurnID:           snapshot.TurnID,
		StepID:           snapshot.StepID,
		StepIndex:        snapshot.StepIndex,
		Generation:       snapshot.Generation,
		LifecycleSeq:     lifecycleSeq,
		Terminal:         snapshot.TurnStatus.Terminal(),
		EndReason:        endReason,
		InteractionKind:  strings.TrimSpace(snapshot.InteractionKind),
		RecoveryRequired: snapshot.RecoveryRequired,
		ToolExecutions:   append([]turn.ToolExecutionView(nil), snapshot.ToolExecutions...),
	}
}

func turnStateEventData(snapshot turn.CoordinatorSnapshot, lifecycleSeq, historyRevision uint64, command turn.CommandType) map[string]any {
	view := buildTurnStateView(snapshot, lifecycleSeq)
	view.HistoryRevision = historyRevision
	return map[string]any{
		"authority":         view.Authority,
		"phase":             view.Phase,
		"turn_status":       view.TurnStatus,
		"step_status":       view.StepStatus,
		"turn_id":           view.TurnID,
		"step_id":           view.StepID,
		"step_index":        view.StepIndex,
		"generation":        view.Generation,
		"lifecycle_seq":     view.LifecycleSeq,
		"history_revision":  view.HistoryRevision,
		"terminal":          view.Terminal,
		"end_reason":        view.EndReason,
		"interaction_kind":  view.InteractionKind,
		"recovery_required": view.RecoveryRequired,
		"tool_executions":   view.ToolExecutions,
		"command":           string(command),
	}
}

func (r *runtime) publishTurnState(snapshot turn.CoordinatorSnapshot, command turn.CommandType) {
	if r == nil || snapshot.TurnID == "" {
		return
	}
	publisher := r.publisher
	if publisher == nil {
		return
	}
	// runtime.agentID is the owning Node ID in production. SSE subscribers
	// filter by the session/Agent ID, which is the wire identity used by the
	// Orchestrator's other events.
	r.mu.Lock()
	historyRevision := r.historyRevision
	r.mu.Unlock()
	publisher.Publish(r.session.ID, "turn_state", turnStateEventData(snapshot, r.lifecycleEventSequence(), historyRevision, command))
}

func turnStatePhase(snapshot turn.CoordinatorSnapshot) string {
	if snapshot.TurnStatus.Terminal() {
		switch snapshot.TurnStatus {
		case turn.TurnStatusCompleted:
			return "completed"
		case turn.TurnStatusFailed:
			return "failed"
		case turn.TurnStatusCancelled:
			return "cancelled"
		case turn.TurnStatusInterrupted:
			return "interrupted"
		case turn.TurnStatusBudgetExhausted:
			return "budget_exhausted"
		}
	}
	if !snapshot.HasActiveTurn || snapshot.TurnID == "" {
		return "idle"
	}
	switch snapshot.StepStatus {
	case turn.StepStatusWaitingInteraction:
		if strings.EqualFold(strings.TrimSpace(snapshot.InteractionKind), "user_information") {
			return "waiting_user"
		}
		return "tool_waiting"
	case turn.StepStatusExecutingTools:
		return "tool_executing"
	case turn.StepStatusCreated, turn.StepStatusReadyForNext:
		return "queued"
	case turn.StepStatusRequesting, turn.StepStatusAssistantReceived:
		return "model_generating"
	default:
		return "queued"
	}
}
