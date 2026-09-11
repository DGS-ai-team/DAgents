package session

import (
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func validDreamingAttempt() DreamingAttempt {
	return DreamingAttempt{AgentID: "agent-1", SessionID: "session-1", TurnID: "turn-7", ExperienceRevision: 4, LocalDate: "2026-09-09", MaxToolRounds: 8, State: DreamingAttemptWaiting}
}

func TestDreamingAttemptCompletionRequiresExactTurnResult(t *testing.T) {
	a := validDreamingAttempt()
	if _, err := a.Complete("turn-other", "assistant-1", "result", "boundary", turn.TurnUsage{}, false, time.Now()); err == nil {
		t.Fatal("different turn was accepted")
	}
	if _, err := a.Complete(a.TurnID, "", "result", "boundary", turn.TurnUsage{}, false, time.Now()); err == nil {
		t.Fatal("missing assistant identity was accepted")
	}
	if _, err := a.Complete(a.TurnID, "assistant-1", "result", "", turn.TurnUsage{}, false, time.Now()); err == nil {
		t.Fatal("missing fixed boundary was accepted")
	}
	completed, err := a.Complete(a.TurnID, "assistant-1", "result", "boundary", turn.TurnUsage{}, false, time.Now())
	if err != nil || completed.State != DreamingAttemptCompleted || completed.AssistantMessageID != "assistant-1" {
		t.Fatalf("completion = %+v, err=%v", completed, err)
	}
	if !strings.Contains(completed.FinalMessage, "result") {
		t.Fatal("exact final message was not retained")
	}
}

func TestDreamingAttemptFailureDoesNotProduceResult(t *testing.T) {
	a := validDreamingAttempt()
	failed, err := a.Fail(DreamingAttemptCancelled, time.Now())
	if err != nil || failed.State != DreamingAttemptCancelled {
		t.Fatalf("cancel = %+v, err=%v", failed, err)
	}
	if _, err := failed.Complete(a.TurnID, "assistant-1", "late", "boundary", turn.TurnUsage{}, true, time.Now()); err == nil {
		t.Fatal("cancelled attempt accepted a late result")
	}
}

func TestDreamingAttemptValidationRejectsMalformedTerminal(t *testing.T) {
	a := validDreamingAttempt()
	a.State = DreamingAttemptCompleted
	if err := a.Validate(); err == nil {
		t.Fatal("completed attempt without exact result was accepted")
	}
}
