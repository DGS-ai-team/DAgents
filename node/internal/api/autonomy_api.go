package api

import (
	"context"
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
	Enabled *bool `json:"enabled,omitempty"`
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
	for _, g := range s.goalStore.List() {
		if g.AgentID == id && g.Managed {
			writeJSON(w, 200, map[string]any{"goal_id": g.ID, "status": g.Status, "limits": g, "progress": g.LastCheckpoint})
			return
		}
	}
	writeJSON(w, 200, map[string]any{"goal_id": "", "status": "disabled", "limits": nil, "progress": nil})
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
	var existing *goals.Goal
	for _, g := range s.goalStore.List() {
		if g.AgentID == id && g.Managed {
			c := g
			existing = &c
			break
		}
	}
	if existing == nil {
		enabled := in.Enabled != nil && *in.Enabled
		g, err := s.createManagedGoal(r.Context(), in.CreateInput, enabled)
		if err != nil {
			writeAPIError(w, 400, "autonomy_create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, 200, map[string]any{"goal_id": g.ID, "status": g.Status, "limits": g, "progress": g.LastCheckpoint})
		return
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
	g, err := s.goalStore.UpdateConfigurationAndStatus(existing.ID, in.CreateInput, status, time.Now().UTC())
	if err != nil {
		writeAPIError(w, 400, "invalid_autonomy", err.Error(), nil)
		return
	}
	if err := s.syncManagedTrigger(g); err != nil {
		_ = s.goalStore.RestoreConfiguration(existing.ID, *existing, time.Now().UTC())
		writeAPIError(w, 409, "autonomy_state_conflict", err.Error(), nil)
		return
	}
	writeJSON(w, 200, map[string]any{"goal_id": g.ID, "status": g.Status, "limits": g, "progress": g.LastCheckpoint})
}
