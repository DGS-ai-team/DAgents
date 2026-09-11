package session

import (
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// DreamingAttemptState is the durable state of one explicitly scheduled
// dreaming turn. It is deliberately separate from TurnSource: other side
// effects use that source too.
type DreamingAttemptState string

const (
	DreamingAttemptRunning   DreamingAttemptState = "running"
	DreamingAttemptWaiting   DreamingAttemptState = "waiting"
	DreamingAttemptCompleted DreamingAttemptState = "completed"
	DreamingAttemptFailed    DreamingAttemptState = "failed"
	DreamingAttemptCancelled DreamingAttemptState = "cancelled"
)

// DreamingAttempt is the small session-owned record needed to resume and
// finalize one dreaming turn without guessing from the latest transcript
// message. Persistence is wired by the runtime owner; this type contains no
// database or scheduler behavior.
type DreamingAttempt struct {
	AgentID                string               `json:"agent_id"`
	SessionID              string               `json:"session_id"`
	TurnID                 string               `json:"turn_id"`
	ExperienceRevision     int64                `json:"experience_revision"`
	LocalDate              string               `json:"local_date"`
	MaxToolRounds          int                  `json:"max_tool_rounds"`
	State                  DreamingAttemptState `json:"state"`
	AssistantMessageID     string               `json:"assistant_message_id,omitempty"`
	FinalMessage           string               `json:"final_message,omitempty"`
	Boundary               string               `json:"boundary,omitempty"`
	HistoryStart           int                  `json:"history_start"`
	HandbookMutationBefore uint64               `json:"handbook_mutation_before"`
	Usage                  turn.TurnUsage       `json:"usage"`
	UsageKnown             bool                 `json:"usage_known"`
	StartedAt              time.Time            `json:"started_at"`
	FinishedAt             time.Time            `json:"finished_at,omitempty"`
}

func (a DreamingAttempt) Validate() error {
	if strings.TrimSpace(a.AgentID) == "" || strings.TrimSpace(a.SessionID) == "" || strings.TrimSpace(a.TurnID) == "" {
		return fmt.Errorf("dreaming attempt identity is required")
	}
	if a.ExperienceRevision < 0 || a.MaxToolRounds <= 0 {
		return fmt.Errorf("invalid dreaming attempt budget or experience revision")
	}
	switch a.State {
	case DreamingAttemptRunning, DreamingAttemptWaiting, DreamingAttemptCompleted, DreamingAttemptFailed, DreamingAttemptCancelled:
	default:
		return fmt.Errorf("invalid dreaming attempt state")
	}
	if a.State == DreamingAttemptCompleted {
		if strings.TrimSpace(a.AssistantMessageID) == "" || strings.TrimSpace(a.FinalMessage) == "" || strings.TrimSpace(a.Boundary) == "" {
			return fmt.Errorf("completed dreaming attempt lacks exact result boundary")
		}
	}
	return nil
}

// Complete records only the assistant message belonging to the attempt's
// exact TurnID. Callers must obtain assistantID and boundary from that turn's
// lifecycle result; no transcript-wide "last message" fallback is provided.
func (a DreamingAttempt) Complete(turnID, assistantID, message, boundary string, usage turn.TurnUsage, usageKnown bool, now time.Time) (DreamingAttempt, error) {
	if err := a.Validate(); err != nil {
		return DreamingAttempt{}, err
	}
	if a.State != DreamingAttemptRunning && a.State != DreamingAttemptWaiting {
		return a, fmt.Errorf("dreaming attempt is not resumable")
	}
	if strings.TrimSpace(turnID) != a.TurnID || strings.TrimSpace(assistantID) == "" || strings.TrimSpace(message) == "" || strings.TrimSpace(boundary) == "" {
		return a, fmt.Errorf("dreaming result does not match attempt")
	}
	a.State = DreamingAttemptCompleted
	a.AssistantMessageID = strings.TrimSpace(assistantID)
	a.FinalMessage = message
	a.Boundary = strings.TrimSpace(boundary)
	a.Usage = usage
	a.UsageKnown = usageKnown
	a.FinishedAt = now
	return a, nil
}

func (a DreamingAttempt) Fail(state DreamingAttemptState, now time.Time) (DreamingAttempt, error) {
	if err := a.Validate(); err != nil {
		return DreamingAttempt{}, err
	}
	if state != DreamingAttemptFailed && state != DreamingAttemptCancelled {
		return a, fmt.Errorf("invalid dreaming terminal state")
	}
	if a.State != DreamingAttemptRunning && a.State != DreamingAttemptWaiting {
		return a, fmt.Errorf("dreaming attempt is not active")
	}
	a.State, a.FinishedAt = state, now
	return a, nil
}
