package api

import (
	"context"
	"fmt"
	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"net/http"
	"strings"
	"time"
)

// createManagedGoal provisions the goal, dedicated session, and managed
// trigger as one API service operation. The initial enabled state is written
// to the goal before any observer can see it.
func (s *Server) createManagedGoal(ctx context.Context, in goals.CreateInput, enabled bool) (goals.Goal, error) {
	return s.createGoal(ctx, in, enabled, true)
}

// provisionManagedCycle attaches the runtime, dedicated session and trigger
// to a cycle that was durably created by the goals store. It is retry-safe:
// existing bindings are reused and no second trigger is created.
func (s *Server) provisionManagedCycle(ctx context.Context, g goals.Goal, enabled bool) (goals.Goal, error) {
	if s.goalStore == nil || s.agents == nil || s.triggerStore == nil {
		return g, fmt.Errorf("managed goal dependencies unavailable")
	}
	if g.ProvisionStatus == "ready" && g.SessionID != "" && g.TriggerID != "" {
		return g, nil
	}
	rec, err := s.agents.Get(ctx, g.AgentID)
	if err != nil || rec == nil || rec.Archived {
		return g, fmt.Errorf("target agent unavailable")
	}
	sid := g.SessionID
	if sid == "" {
		sid = "goal-session-" + g.ID
	}
	if err := s.ensureGoalRuntime(ctx, *rec, sid); err != nil {
		return g, err
	}
	now := time.Now().UTC()
	if g.SessionID == "" {
		before := g
		updated, bindErr := s.goalStore.BindSession(g.ID, sid, now)
		if bindErr != nil {
			return before, bindErr
		}
		g = updated
	}
	if g.TriggerID != "" {
		tr, ok := s.triggerStore.GetTrigger(g.TriggerID)
		if !ok || tr.ManagedGoalID != g.ID || tr.OwnerAgentID != g.AgentID || tr.Controller != "goal" || tr.ControllerID != g.ID || tr.TargetAgentID != g.AgentID || tr.TargetSessionID == nil || *tr.TargetSessionID != sid {
			return g, fmt.Errorf("managed trigger identity conflict")
		}
	}
	if g.TriggerID == "" {
		interval := g.MinWakeIntervalSeconds
		if interval < 60 {
			interval = 300
		}
		vEnabled := false // enable only after both durable bindings exist
		def, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{
			Name:          "managed-goal-" + g.ID,
			Condition:     map[string]any{"interval_seconds": interval},
			TargetAgentID: g.AgentID, TargetSessionID: &sid,
			TaskTemplate: g.Objective, Enabled: &vEnabled,
		}, s.cfg.NodeID, now)
		if err != nil {
			return g, err
		}
		def.ManagedGoalID, def.OwnerAgentID, def.Controller, def.ControllerID, def.CreatedBy = g.ID, g.AgentID, "goal", g.ID, "autonomy"
		if g.NextWakeAt != nil {
			v := float64(g.NextWakeAt.UnixNano()) / 1e9
			def.NextFireAt = &v
		}
		def.TriggerID = "managed-trigger-" + g.ID
		if existing, ok := s.triggerStore.GetTrigger(def.TriggerID); ok {
			if existing.ManagedGoalID != g.ID || existing.OwnerAgentID != g.AgentID || existing.Controller != "goal" || existing.ControllerID != g.ID || existing.TargetAgentID != g.AgentID || existing.TargetSessionID == nil || *existing.TargetSessionID != sid {
				return g, fmt.Errorf("managed trigger identity conflict")
			}
		} else if _, err = s.triggerStore.CreateTrigger(def); err != nil {
			return g, err
		}
		beforeBind := g
		g, err = s.goalStore.BindTrigger(g.ID, def.TriggerID, now)
		if err != nil {
			return beforeBind, err
		}
	}
	if enabled && g.Status == goals.StatusPaused && (g.ProvisionStatus == "pending" || g.StatusReason == "provisioning_failed") {
		before := g
		updated, setErr := s.goalStore.SetStatus(g.ID, goals.StatusActive, now)
		if setErr != nil {
			return before, setErr
		}
		g = updated
	}
	if err := s.syncManagedTrigger(g); err != nil {
		return g, err
	}
	if g.ProvisionStatus != "ready" {
		if ready, e := s.goalStore.SetProvisionStatus(g.ID, "ready", now); e == nil {
			g = ready
		} else {
			return g, e
		}
	}
	return g, nil
}

func (s *Server) createGoal(ctx context.Context, in goals.CreateInput, enabled, managed bool) (goals.Goal, error) {
	if s.goalStore == nil || s.agents == nil || s.triggerStore == nil {
		return goals.Goal{}, fmt.Errorf("managed goal dependencies unavailable")
	}
	in.AgentID = strings.TrimSpace(in.AgentID)
	in.Managed = managed
	rec, err := s.agents.Get(ctx, in.AgentID)
	if err != nil || rec == nil || rec.Archived {
		return goals.Goal{}, fmt.Errorf("target agent unavailable")
	}
	snap, snapErr := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if snapErr != nil {
		return goals.Goal{}, fmt.Errorf("agent snapshot invalid: %w", snapErr)
	}
	if snap.AgentType != "auto" {
		return goals.Goal{}, fmt.Errorf("autonomy requires an auto Agent")
	}
	var g goals.Goal
	if managed {
		g, err = s.goalStore.CreateManaged(in, time.Now().UTC(), enabled)
	} else {
		g, err = s.goalStore.Create(in, time.Now().UTC())
	}
	if err != nil {
		return goals.Goal{}, err
	}
	goalID := g.ID
	committed := false
	defer func() {
		if !committed {
			_ = s.goalStore.Delete(goalID)
		}
	}()
	now := time.Now().UTC()
	sid := "goal-session-" + g.ID
	if err := s.ensureGoalRuntime(ctx, *rec, sid); err != nil {
		return goals.Goal{}, err
	}
	if g, err = s.goalStore.BindSession(g.ID, sid, now); err != nil {
		return goals.Goal{}, err
	}
	interval := g.MinWakeIntervalSeconds
	if interval < 60 {
		interval = 300
	}
	def, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "managed-goal-" + g.ID, Condition: map[string]any{"interval_seconds": interval}, TargetAgentID: g.AgentID, TargetSessionID: &sid, TaskTemplate: g.Objective, Enabled: func() *bool { v := enabled; return &v }()}, s.cfg.NodeID, now)
	if err != nil {
		return goals.Goal{}, err
	}
	def.ManagedGoalID = g.ID
	def.Controller = "goal"
	def.ControllerID = g.ID
	def.CreatedBy = "autonomy"
	if g.NextWakeAt != nil {
		v := float64(g.NextWakeAt.UnixNano()) / 1e9
		def.NextFireAt = &v
	}
	if _, err = s.triggerStore.CreateTrigger(def); err != nil {
		return goals.Goal{}, err
	}
	if g, err = s.goalStore.BindTrigger(g.ID, def.TriggerID, now); err != nil {
		_ = s.triggerStore.DeleteTrigger(def.TriggerID)
		return goals.Goal{}, err
	}
	committed = true
	return g, nil
}

func (s *Server) registerGoalRoutes() {
	s.mux.HandleFunc("GET /v1/goals", s.handleListGoals)
	s.mux.HandleFunc("POST /v1/goals", s.handleCreateGoal)
	s.mux.HandleFunc("GET /v1/goals/{goal_id}", s.handleGetGoal)
	s.mux.HandleFunc("POST /v1/goals/{goal_id}/pause", s.handlePauseGoal)
	s.mux.HandleFunc("POST /v1/goals/{goal_id}/resume", s.handleResumeGoal)
	s.mux.HandleFunc("POST /v1/goals/{goal_id}/stop", s.handleStopGoal)
	s.mux.HandleFunc("POST /v1/goals/{goal_id}/wake", s.handleWakeGoal)
	s.mux.HandleFunc("GET /v1/goals/{goal_id}/runs", s.handleGoalRuns)
}
func (s *Server) requireGoalStore(w http.ResponseWriter) *goals.Store {
	if s.goalStore == nil {
		writeAPIError(w, 503, "goals_unavailable", "goal store is unavailable", nil)
		return nil
	}
	return s.goalStore
}
func (s *Server) handleListGoals(w http.ResponseWriter, _ *http.Request) {
	st := s.requireGoalStore(w)
	if st != nil {
		writeJSON(w, 200, map[string]any{"goals": st.List()})
	}
}
func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request) {
	st := s.requireGoalStore(w)
	if st == nil {
		return
	}
	var in goals.CreateInput
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_json", err.Error(), nil)
		return
	}
	if s.agents == nil {
		writeAPIError(w, 503, "agents_unavailable", "agent store unavailable", nil)
		return
	}
	rec, err := s.agents.Get(r.Context(), strings.TrimSpace(in.AgentID))
	if err != nil || rec == nil || rec.Archived {
		writeAPIError(w, 400, "agent_not_found", "target agent unavailable", nil)
		return
	}
	writeAPIError(w, http.StatusConflict, "use_agent_autonomy", "configure autonomous work through PUT /v1/agents/{agent_id}/autonomy", nil)
}
func (s *Server) handleGetGoal(w http.ResponseWriter, r *http.Request) {
	st := s.requireGoalStore(w)
	if st == nil {
		return
	}
	g, ok := st.Get(strings.TrimSpace(r.PathValue("goal_id")))
	if !ok {
		writeAPIError(w, 404, "not_found", "goal not found", nil)
		return
	}
	writeJSON(w, 200, g)
}
func (s *Server) changeGoalStatus(w http.ResponseWriter, r *http.Request, status goals.Status) {
	st := s.requireGoalStore(w)
	if st == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("goal_id"))
	current, ok := st.Get(id)
	if !ok {
		writeAPIError(w, 404, "not_found", "goal not found", nil)
		return
	}
	if status == goals.StatusActive {
		s.goalWakeMu.Lock()
		defer s.goalWakeMu.Unlock()
		current, ok = st.Get(id)
		if !ok || !s.managedGoalAuto(r.Context(), current) {
			writeAPIError(w, http.StatusConflict, "agent_not_auto", "managed Goal requires an auto Agent", nil)
			return
		}
	}
	g, err := st.SetStatus(id, status, time.Now())
	if err == goals.ErrNotFound {
		writeAPIError(w, 404, "not_found", "goal not found", nil)
		return
	}
	if err != nil {
		writeAPIError(w, 409, "goal_state_conflict", err.Error(), nil)
		return
	}
	if g.Managed {
		if syncErr := s.syncManagedTrigger(g); syncErr != nil {
			writeAPIError(w, 409, "goal_state_conflict", syncErr.Error(), nil)
			return
		}
	}
	writeJSON(w, 200, g)
}
func (s *Server) handlePauseGoal(w http.ResponseWriter, r *http.Request) {
	s.changeGoalStatus(w, r, goals.StatusPaused)
}
func (s *Server) handleResumeGoal(w http.ResponseWriter, r *http.Request) {
	s.changeGoalStatus(w, r, goals.StatusActive)
}
func (s *Server) handleStopGoal(w http.ResponseWriter, r *http.Request) {
	if s.goalStore != nil {
		if g, ok := s.goalStore.Get(strings.TrimSpace(r.PathValue("goal_id"))); ok {
			updated, err := s.goalStore.SetStatus(g.ID, goals.StatusStopped, time.Now())
			if err != nil {
				writeAPIError(w, 409, "goal_state_conflict", err.Error(), nil)
				return
			}
			if s.sessions != nil && updated.SessionID != "" && !s.sessions.CancelTurn(updated.SessionID) { /* no active Turn */
			}
			if updated.Managed {
				if syncErr := s.syncManagedTrigger(updated); syncErr != nil {
					writeAPIError(w, 409, "goal_state_conflict", syncErr.Error(), nil)
					return
				}
			}
			writeJSON(w, 200, updated)
			return
		}
	}
	s.changeGoalStatus(w, r, goals.StatusStopped)
}
func (s *Server) handleGoalRuns(w http.ResponseWriter, r *http.Request) {
	st := s.requireGoalStore(w)
	if st == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("goal_id"))
	if _, ok := st.Get(id); !ok {
		writeAPIError(w, 404, "not_found", "goal not found", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"runs": st.Runs(id)})
}
func (s *Server) handleWakeGoal(w http.ResponseWriter, r *http.Request) {
	st := s.requireGoalStore(w)
	if st == nil {
		return
	}
	if s.goalWake == nil {
		writeAPIError(w, 503, "goal_runner_unavailable", "goal runtime is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("goal_id"))
	g, ok := st.Get(id)
	if !ok {
		writeAPIError(w, 404, "not_found", "goal not found", nil)
		return
	}
	if s.goalAgentBusy(g) {
		writeAPIError(w, 409, "agent_busy", "agent has an active turn or queued input", nil)
		return
	}
	s.goalWakeMu.Lock()
	defer s.goalWakeMu.Unlock()
	current, ok := st.Get(id)
	if !ok || !s.managedGoalAuto(r.Context(), current) {
		writeAPIError(w, http.StatusConflict, "agent_not_auto", "managed Goal requires an auto Agent", nil)
		return
	}
	g = current
	if s.goalAgentBusy(g) {
		writeAPIError(w, 409, "agent_busy", "agent has an active turn or queued input", nil)
		return
	}
	run, err := st.StartRun(id, "manual", time.Now())
	if err != nil {
		writeAPIError(w, 409, "goal_not_runnable", err.Error(), nil)
		return
	}
	delivery, err := s.goalWake(r.Context(), g, run)
	if err != nil || strings.TrimSpace(delivery) == "" {
		run.Status = "failed"
		run.Reason = "wake delivery failed"
		_, _ = st.FinishRun(id, run, time.Now())
		if err == nil {
			err = goals.ErrNotRunnable
		}
		writeAPIError(w, 502, "goal_wake_failed", err.Error(), nil)
		return
	}
	writeJSON(w, 202, map[string]any{"run": run, "delivery_id": delivery})
}
