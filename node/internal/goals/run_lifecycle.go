package goals

import (
	"fmt"
	"strings"
	"time"
)

// TurnSnapshot is a dependency-free projection supplied by the session
// lifecycle observer. Keeping this DTO here avoids a goals -> turn -> tools
// import cycle.
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

// ObserveTurn projects the authoritative session Turn into its active Goal
// Run. It is idempotent: once a Run has a FinishedAt timestamp, later
// lifecycle snapshots cannot charge it or revive a stopped Goal.
func (s *Store) ObserveTurn(sessionID string, snapshot TurnSnapshot, now time.Time) error {
	if s == nil || strings.TrimSpace(sessionID) == "" || snapshot.TurnID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var goalID string
	var idx = -1
	for id, goal := range s.data.Goals {
		if goal.SessionID != sessionID {
			continue
		}
		for i := range s.data.Runs[id] {
			run := s.data.Runs[id][i]
			if run.FinishedAt == nil && (run.TurnID == "" || run.TurnID == snapshot.TurnID) {
				goalID, idx = id, i
				break
			}
		}
		if goalID != "" {
			break
		}
	}
	if goalID == "" || idx < 0 {
		return nil
	}
	g := s.data.Goals[goalID]
	r := s.data.Runs[goalID][idx]
	priorGoalStatus := g.Status
	priorGoalReason := g.StatusReason
	oldGoal, oldRun := g, r
	if r.TurnID == "" {
		r.TurnID = snapshot.TurnID
		s.data.Runs[goalID][idx] = r
		if err := s.saveLocked(); err != nil {
			s.data.Runs[goalID][idx] = oldRun
			return err
		}
	}
	if snapshot.StepStatus == "waiting_for_interaction" && g.Status != StatusStopped && g.Status != StatusCompleted {
		g.Status = StatusWaiting
		g.StatusReason = "approval_required"
		g.UpdatedAt = now.UTC()
		s.data.Goals[goalID] = g
		if err := s.saveLocked(); err != nil {
			s.data.Goals[goalID] = oldGoal
			s.data.Runs[goalID][idx] = oldRun
			return err
		}
		return nil
	}
	terminal := snapshot.TurnStatus == "completed" || snapshot.TurnStatus == "failed" || snapshot.TurnStatus == "cancelled" || snapshot.TurnStatus == "interrupted" || snapshot.TurnStatus == "budget_exhausted"
	if !terminal {
		return nil
	}
	if g.Status == StatusCompleted {
		return nil
	}
	r.TokensUsed = int64(snapshot.TotalTokens)
	if snapshot.TotalTokens <= 0 {
		r.Status = "unknown"
		r.Reason = "usage_unknown"
		finished := now.UTC()
		r.FinishedAt = &finished
		s.data.Runs[goalID][idx] = r
		if priorGoalStatus == StatusStopped {
			g.Status, g.StatusReason = StatusStopped, priorGoalReason
		} else {
			g.Status, g.StatusReason = StatusPaused, "usage_unknown"
		}
		g.UpdatedAt = finished
		s.data.Goals[goalID] = g
		if err := s.saveLocked(); err != nil {
			s.data.Goals[goalID] = oldGoal
			s.data.Runs[goalID][idx] = oldRun
			return fmt.Errorf("save observed lifecycle: %w", err)
		}
		return nil
	}
	switch snapshot.TurnStatus {
	case "completed":
		r.Status = "completed"
	case "cancelled":
		r.Status = "cancelled"
	case "interrupted":
		r.Status = "interrupted"
	default:
		r.Status = "failed"
	}
	reason := snapshot.TurnEndReason
	if reason == "" {
		reason = snapshot.StepEndReason
	}
	r.Reason = reason
	finished := now.UTC()
	r.FinishedAt = &finished
	s.data.Runs[goalID][idx] = r
	g.TokensUsed += r.TokensUsed
	if r.Checkpoint != nil {
		g.LastCheckpoint = r.Checkpoint
	}
	if g.Status == StatusStopped {
		// Preserve an explicit user stop; a late successful Turn cannot revive it.
	} else if r.Status == "completed" && checkpointProvesCompletion(r.Checkpoint) && priorGoalStatus != StatusPaused {
		g.Status, g.StatusReason = StatusCompleted, ""
	} else if r.Status == "completed" && priorGoalStatus != StatusPaused {
		// A prior approval_required reason describes the finished Turn, not the
		// next Goal state. Clear it when the Turn completed and needs another wake.
		g.Status, g.StatusReason = StatusWaiting, ""
	} else if g.Status != StatusStopped && priorGoalStatus != StatusPaused {
		g.Status = StatusFailed
	}
	if r.Status == "completed" && !checkpointProvesCompletion(r.Checkpoint) {
		progress := checkpointProgress(r.Checkpoint)
		streak := 1
		for i := len(s.data.Runs[goalID]) - 1; i >= 0 && streak < 2; i-- {
			prior := s.data.Runs[goalID][i]
			if prior.ID == r.ID || prior.Status != "completed" {
				continue
			}
			if checkpointProgress(prior.Checkpoint) == progress {
				streak++
			} else {
				break
			}
		}
		if streak >= 2 && g.Status != StatusStopped {
			g.Status, g.StatusReason = StatusPaused, "no_progress"
		}
	}
	if snapshot.TurnStatus == "budget_exhausted" && priorGoalStatus != StatusStopped && priorGoalStatus != StatusPaused {
		g.Status, g.StatusReason = StatusPaused, "budget_exhausted"
	}
	g.UpdatedAt = finished
	s.data.Goals[goalID] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[goalID] = oldGoal
		s.data.Runs[goalID][idx] = oldRun
		return fmt.Errorf("save observed lifecycle: %w", err)
	}
	return nil
}
