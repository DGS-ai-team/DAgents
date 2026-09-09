package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
)

type autonomyActionRequest struct {
	Action                  string `json:"action"`
	ExpectedProfileRevision *int64 `json:"expected_profile_revision"`
	ExpectedGoalRevision    *int64 `json:"expected_goal_revision,omitempty"`
}

func (s *Server) handleAutonomyAction(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	s.goalWakeMu.Lock()
	defer s.goalWakeMu.Unlock()
	if !s.autoAgent(id, r, w) {
		return
	}
	if s.goalStore == nil {
		writeAPIError(w, 503, "goals_unavailable", "goal store unavailable", nil)
		return
	}
	var in autonomyActionRequest
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_json", err.Error(), nil)
		return
	}
	if in.ExpectedProfileRevision == nil {
		writeAPIError(w, 400, "expected_profile_revision_required", "expected_profile_revision is required", nil)
		return
	}
	p, g, err := s.currentAutoGoal(id, time.Now().UTC())
	if err != nil {
		writeAPIError(w, 409, "recovery_required", err.Error(), nil)
		return
	}
	if p.AgentID == "" {
		writeAPIError(w, 409, "profile_required", "configure autonomy first", nil)
		return
	}
	now := time.Now().UTC()
	action := strings.TrimSpace(in.Action)
	if action != "pause_goal" && action != "resume_goal" && action != "disable_auto" && action != "enable_auto" {
		writeAPIError(w, 400, "invalid_action", "unsupported autonomy action", nil)
		return
	}
	if g == nil {
		if action == "pause_goal" || action == "resume_goal" {
			writeAPIError(w, 409, "cycle_required", "no current cycle", nil)
			return
		}
		q, _, err := s.goalStore.ApplyAutoAction(id, "", action, *in.ExpectedProfileRevision, 0, now)
		if err != nil {
			writeAPIError(w, 409, "profile_conflict", err.Error(), nil)
			return
		}
		if action == "disable_auto" && s.maintenanceSched != nil {
			s.maintenanceSched.Cancel(id)
		}
		u, _ := s.goalStore.GetUsage(id)
		writeJSON(w, 200, s.autonomyPayload(q, nil, u))
		return
	}
	if in.ExpectedGoalRevision == nil {
		writeAPIError(w, 400, "expected_goal_revision_required", "expected_goal_revision is required", nil)
		return
	}
	q, updated, err := s.goalStore.ApplyAutoAction(id, g.ID, action, *in.ExpectedProfileRevision, *in.ExpectedGoalRevision, now)
	if err != nil {
		if errors.Is(err, goals.ErrEventSchedulingUnsupported) {
			writeAPIError(w, http.StatusConflict, "event_scheduling_unsupported", err.Error(), map[string]any{"goal_id": g.ID})
			return
		}
		writeAPIError(w, 409, "action_conflict", err.Error(), nil)
		return
	}
	if action == "disable_auto" {
		if s.maintenanceSched != nil {
			s.maintenanceSched.Cancel(id)
		}
	}
	if (action == "disable_auto" || action == "pause_goal") && s.sessions != nil && updated.SessionID != "" {
		_ = s.sessions.CancelTurn(updated.SessionID)
	}
	if err = s.syncManagedTrigger(updated); err != nil {
		writeAPIError(w, 409, "projection_pending", err.Error(), map[string]any{"goal_id": updated.ID})
		return
	}
	u, _ := s.goalStore.GetUsage(id)
	writeJSON(w, 200, s.autonomyPayload(q, &updated, u))
}
