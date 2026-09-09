package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"net/http"
	"strings"
	"time"
)

func (s *Server) syncManagedTrigger(g goals.Goal) error {
	if s.triggerStore == nil || g.TriggerID == "" {
		return nil
	}
	enabled := g.Status == goals.StatusActive || g.Status == goals.StatusWaiting
	var next *float64
	if enabled && g.NextWakeAt != nil {
		v := float64(g.NextWakeAt.UnixNano()) / 1e9
		next = &v
	}
	_, err := s.triggerStore.UpdateManagedConfig(g.TriggerID, g.MinWakeIntervalSeconds, enabled, g.Objective, next, time.Now().UTC())
	return err
}

type autonomyInput struct {
	goals.CreateInput
	Enabled              *bool              `json:"enabled,omitempty"`
	Profile              *goals.AutoProfile `json:"profile,omitempty"`
	ExpectedRevision     *int64             `json:"expected_revision,omitempty"`
	ExpectedGoalRevision *int64             `json:"expected_goal_revision,omitempty"`
}

// currentAutoGoal is the single binding point for the Auto profile. Legacy
// managed data is migrated only after the caller has authenticated the Agent
// as Auto; a migration issue is surfaced instead of guessing a Goal.
func (s *Server) currentAutoGoal(agentID string, now time.Time) (goals.AutoProfile, *goals.Goal, error) {
	if s.goalStore == nil {
		return goals.AutoProfile{}, nil, fmt.Errorf("goals unavailable")
	}
	p, ok := s.goalStore.GetProfile(agentID)
	if !ok {
		valid := map[string]bool{agentID: true}
		if _, err := s.goalStore.MigrateAutoProfiles(valid, now); err != nil {
			return goals.AutoProfile{}, nil, err
		}
		p, ok = s.goalStore.GetProfile(agentID)
	}
	if !ok {
		for _, issue := range s.goalStore.MigrationIssues() {
			if issue.AgentID == agentID {
				return goals.AutoProfile{}, nil, fmt.Errorf("recovery_required: %s", issue.Reason)
			}
		}
		return goals.AutoProfile{}, nil, nil
	}
	if p.CurrentGoalID == "" {
		return p, nil, nil
	}
	g, exists := s.goalStore.Get(p.CurrentGoalID)
	if !exists || !g.Managed || g.AgentID != agentID {
		return goals.AutoProfile{}, nil, fmt.Errorf("recovery_required: profile current cycle is missing")
	}
	return p, &g, nil
}

func (s *Server) autonomyPayload(p goals.AutoProfile, g *goals.Goal, usage goals.AgentUsage) map[string]any {
	summary := s.projectAutoSummary(p.AgentID, p, g)
	if g == nil {
		return map[string]any{"goal_id": "", "status": "disabled", "limits": nil, "progress": nil, "schema_version": 2, "profile": p, "current_cycle": nil, "usage": usage, "summary": summary}
	}
	return map[string]any{"goal_id": g.ID, "status": g.Status, "limits": g, "progress": g.LastCheckpoint, "schema_version": 2, "profile": p, "current_cycle": g, "usage": usage, "summary": summary}
}

func (s *Server) autoAgent(id string, r *http.Request, w http.ResponseWriter) bool {
	if s.agents == nil {
		writeAPIError(w, 503, "agents_unavailable", "agents store unavailable", nil)
		return false
	}
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil || rec == nil || rec.Archived {
		s.writeAgentNotFound(w, id)
		return false
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil || snap.AgentType != "auto" {
		writeAPIError(w, 409, "agent_not_auto", "autonomy requires an auto Agent", nil)
		return false
	}
	return true
}

func (s *Server) managedGoalAuto(ctx context.Context, g goals.Goal) bool {
	if !g.Managed || s.agents == nil {
		return false
	}
	rec, err := s.agents.Get(ctx, g.AgentID)
	if err != nil || rec == nil || rec.Archived {
		return false
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	return err == nil && snap.AgentType == "auto"
}

func (s *Server) handleGetAgentAutonomy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return
	}
	if s.goalStore == nil {
		writeAPIError(w, 503, "goals_unavailable", "goal store unavailable", nil)
		return
	}
	p, g, err := s.currentAutoGoal(id, time.Now().UTC())
	if err != nil {
		writeAPIError(w, 409, "recovery_required", err.Error(), nil)
		return
	}
	u, _ := s.goalStore.GetUsage(id)
	writeJSON(w, 200, s.autonomyPayload(p, g, u))
}

func (s *Server) handlePutAgentAutonomy(w http.ResponseWriter, r *http.Request) {
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
	var in autonomyInput
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_json", err.Error(), nil)
		return
	}
	in.AgentID = id
	p, existing, err := s.currentAutoGoal(id, time.Now().UTC())
	if err != nil {
		writeAPIError(w, 409, "recovery_required", err.Error(), nil)
		return
	}
	beforeProfile := p
	if existing == nil {
		if p.AgentID == "" {
			p = goals.AutoProfile{AgentID: id, PlanMode: "one_shot", Enabled: false}
		}
		if in.Profile != nil {
			q := *in.Profile
			q.AgentID = id
			p = q
			if p.PlanMode == "" {
				p.PlanMode = "one_shot"
			}
		}
		expected := int64(0)
		if in.ExpectedRevision != nil {
			expected = *in.ExpectedRevision
		}
		if in.ExpectedRevision == nil && p.Revision != 0 {
			expected = p.Revision
		}
		if err := s.bindRecurringAuthorization(r.Context(), id, &p); err != nil {
			writeAPIError(w, 409, "configuration_required", err.Error(), nil)
			return
		}
		p, err = s.goalStore.SaveProfile(p, expected, time.Now().UTC())
		if err != nil {
			writeAPIError(w, 409, "profile_conflict", err.Error(), nil)
			return
		}
		if s.maintenanceSched != nil && maintenanceProfileChanged(beforeProfile, p) {
			s.maintenanceSched.Cancel(id)
		}
		// A profile-only PUT is a durable draft; it does not create or
		// implicitly enable a business cycle.
		if in.Profile != nil && strings.TrimSpace(in.Objective) == "" && strings.TrimSpace(in.Acceptance) == "" {
			u, _ := s.goalStore.GetUsage(id)
			writeJSON(w, 200, s.autonomyPayload(p, nil, u))
			return
		}
		enabled := in.Enabled != nil && *in.Enabled
		in.CreateInput.AgentID = id
		in.CreateInput.Managed = true
		in.CreateInput.EnabledIntent = enabled
		key := "put-" + id
		g, e := s.goalStore.CreateManagedCycle(in.CreateInput, key, p.Revision, time.Now().UTC(), false)
		if e != nil {
			writeAPIError(w, 409, "autonomy_create_failed", e.Error(), nil)
			return
		}
		g, e = s.provisionManagedCycle(r.Context(), g, enabled)
		if e != nil {
			_, _ = s.goalStore.SetProvisionStatus(g.ID, "failed", time.Now().UTC())
			writeAPIError(w, 409, "provisioning_failed", e.Error(), nil)
			return
		}
		p, _ = s.goalStore.GetProfile(id)
		if enabled && !p.Enabled {
			p.Enabled = true
			var saveErr error
			p, saveErr = s.goalStore.SaveProfile(p, p.Revision, time.Now().UTC())
			if saveErr != nil {
				writeAPIError(w, 409, "profile_conflict", saveErr.Error(), nil)
				return
			}
		}
		u, _ := s.goalStore.GetUsage(id)
		writeJSON(w, 200, s.autonomyPayload(p, gPtr(g), u))
		return
	}
	var requestedProfile *goals.AutoProfile
	expectedProfileRevision := p.Revision
	if in.ExpectedRevision != nil {
		expectedProfileRevision = *in.ExpectedRevision
	}
	// Validate run state before any profile mutation so a rejected update is
	// observationally side-effect free.
	for _, run := range s.goalStore.Runs(existing.ID) {
		if run.FinishedAt == nil || run.Status == "unknown" {
			writeAPIError(w, 409, "autonomy_state_conflict", "goal has an unresolved run", nil)
			return
		}
	}
	if in.Profile != nil {
		q := *in.Profile
		q.AgentID = id
		q.CurrentGoalID = ""
		requestedProfile = &q
		if err := s.bindRecurringAuthorization(r.Context(), id, requestedProfile); err != nil {
			writeAPIError(w, 409, "configuration_required", err.Error(), nil)
			return
		}
		if strings.TrimSpace(in.Objective) == "" && strings.TrimSpace(in.Acceptance) == "" && in.Enabled == nil {
			p, err = s.goalStore.SaveProfile(q, expectedProfileRevision, time.Now().UTC())
			if err != nil {
				writeAPIError(w, 409, "profile_conflict", err.Error(), nil)
				return
			}
			if s.maintenanceSched != nil && maintenanceProfileChanged(beforeProfile, p) {
				s.maintenanceSched.Cancel(id)
			}
			u, _ := s.goalStore.GetUsage(id)
			writeJSON(w, 200, s.autonomyPayload(p, existing, u))
			return
		}
	}
	for _, run := range s.goalStore.Runs(existing.ID) {
		if run.FinishedAt == nil || run.Status == "unknown" {
			writeAPIError(w, 409, "autonomy_state_conflict", "goal has an unresolved run", nil)
			return
		}
	}
	status := (*goals.Status)(nil)
	if in.Enabled != nil {
		v := goals.StatusPaused
		if *in.Enabled {
			v = goals.StatusActive
		}
		status = &v
	}
	if requestedProfile == nil {
		requestedProfile = &p
	}
	if in.Enabled != nil && in.Profile == nil {
		requestedProfile.Enabled = *in.Enabled
	}
	var g goals.Goal
	expectedGoalRevision := existing.Revision
	if in.ExpectedGoalRevision != nil {
		expectedGoalRevision = *in.ExpectedGoalRevision
	}
	p, g, err = s.goalStore.UpdateProfileAndCycle(id, existing.ID, *requestedProfile, expectedProfileRevision, expectedGoalRevision, in.CreateInput, status, time.Now().UTC())
	if err != nil {
		if errors.Is(err, goals.ErrConflict) {
			writeAPIError(w, 409, "autonomy_revision_conflict", "autonomy configuration is stale", nil)
		} else {
			writeAPIError(w, 400, "invalid_autonomy", err.Error(), nil)
		}
		return
	}
	if s.maintenanceSched != nil && maintenanceProfileChanged(beforeProfile, p) {
		s.maintenanceSched.Cancel(id)
	}
	if err := s.syncManagedTrigger(g); err != nil {
		writeAPIError(w, 409, "autonomy_state_conflict", err.Error(), nil)
		return
	}
	u, _ := s.goalStore.GetUsage(id)
	writeJSON(w, 200, s.autonomyPayload(p, gPtr(g), u))
}

func gPtr(g goals.Goal) *goals.Goal { return &g }
