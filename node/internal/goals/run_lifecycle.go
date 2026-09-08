package goals

import (
	"strings"
	"time"
)

type TurnSnapshot struct {
	TurnID        string
	TurnStatus    string
	StepStatus    string
	TurnEndReason string
	StepEndReason string
	TotalTokens   int
}

func checkpointProvesCompletion(cp *Checkpoint) bool {
	if cp == nil || !cp.Done {
		return false
	}
	for _, e := range cp.Evidence {
		if strings.TrimSpace(e) != "" {
			return true
		}
	}
	return false
}
func checkpointProgress(cp *Checkpoint) string {
	if cp == nil {
		return ""
	}
	return strings.Join([]string{cp.Summary, strings.Join(cp.Completed, "\x00"), strings.Join(cp.Evidence, "\x00"), strings.Join(cp.Artifacts, "\x00")}, "\x01")
}
func terminalTurn(status string) bool {
	switch status {
	case "completed", "failed", "cancelled", "interrupted", "budget_exhausted":
		return true
	}
	return false
}

// ObserveTurn commits terminal lifecycle state through the same transaction
// used by explicit FinalizeRun.
func (s *Store) ObserveTurn(sessionID string, snapshot TurnSnapshot, now time.Time) error {
	if s == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(snapshot.TurnID) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var gid string
	idx := -1
	for id, g := range s.data.Goals {
		if g.SessionID != sessionID {
			continue
		}
		for i, r := range s.data.Runs[id] {
			if r.FinishedAt == nil && (r.TurnID == "" || r.TurnID == snapshot.TurnID) {
				gid, idx = id, i
				break
			}
		}
		if gid != "" {
			break
		}
	}
	if gid == "" {
		return nil
	}
	g := s.data.Goals[gid]
	r := s.data.Runs[gid][idx]
	originalRun := r
	if r.TurnID == "" {
		r.TurnID = snapshot.TurnID
		s.data.Runs[gid][idx] = r
	}
	if snapshot.StepStatus == "waiting_for_interaction" && !terminalTurn(snapshot.TurnStatus) && g.Status != StatusStopped && g.Status != StatusCompleted {
		old := g
		g.Status, g.StatusReason, g.UpdatedAt = StatusWaiting, "approval_required", now.UTC()
		g.Revision++
		s.data.Goals[gid] = g
		if err := s.saveLocked(); err != nil {
			s.data.Goals[gid] = old
			s.data.Runs[gid][idx] = originalRun
			return err
		}
		return nil
	}
	if !terminalTurn(snapshot.TurnStatus) {
		if originalRun.TurnID == "" {
			if err := s.saveLocked(); err != nil {
				s.data.Runs[gid][idx] = originalRun
				return err
			}
		}
		return nil
	}
	decision := FinalDecision{}
	if r.Checkpoint != nil && r.Checkpoint.Decision != nil {
		decision = r.Checkpoint.Decision.Clone()
	}
	reason := snapshot.TurnEndReason
	if strings.TrimSpace(reason) == "" {
		reason = snapshot.StepEndReason
	}
	in := FinalizeInput{RunID: r.ID, ExpectedGoalRevision: r.GoalRevision, ExpectedProfileRevision: r.ProfileRevision, ExpectedConfigRevision: r.ConfigRevision, Decision: decision, ActualTokens: int64(snapshot.TotalTokens), Purpose: "goal", Generation: r.Generation, TerminalStatus: snapshot.TurnStatus, TerminalReason: reason, Now: now.UTC()}
	if err := s.finalizeRunLocked(in); err != nil {
		// The turn binding is part of the same logical observation; do not leave
		// it behind when the terminal transaction cannot be persisted.
		s.data.Runs[gid][idx] = originalRun
		return err
	}
	return nil
}
