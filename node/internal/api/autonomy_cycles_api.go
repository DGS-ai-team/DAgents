package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
)

type autonomyCycleInput struct {
	goals.CreateInput
	IdempotencyKey          string `json:"idempotency_key"`
	ExpectedProfileRevision *int64 `json:"expected_profile_revision,omitempty"`
	Enabled                 *bool  `json:"enabled,omitempty"`
}

func (s *Server) handleGetAutonomyCycles(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return
	}
	if s.goalStore == nil {
		writeAPIError(w, 503, "goals_unavailable", "goal store unavailable", nil)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	all := make([]goals.Goal, 0)
	for _, g := range s.goalStore.List() {
		if g.Managed && g.AgentID == id {
			all = append(all, g)
		}
	}
	total := len(all)
	if total == 0 {
		page = 1
	} else if page > (total+pageSize-1)/pageSize {
		page = (total + pageSize - 1) / pageSize
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	writeJSON(w, 200, map[string]any{"items": all[start:end], "total": total, "page": page, "page_size": pageSize})
}

func (s *Server) handlePostAutonomyCycle(w http.ResponseWriter, r *http.Request) {
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
	var in autonomyCycleInput
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_json", err.Error(), nil)
		return
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		writeAPIError(w, 400, "idempotency_key_required", "idempotency_key is required", nil)
		return
	}
	if in.ExpectedProfileRevision == nil {
		writeAPIError(w, 400, "expected_profile_revision_required", "expected_profile_revision is required", nil)
		return
	}
	p, _, err := s.currentAutoGoal(id, time.Now().UTC())
	if err != nil {
		writeAPIError(w, 409, "recovery_required", err.Error(), nil)
		return
	}
	if p.AgentID == "" {
		writeAPIError(w, http.StatusConflict, "profile_required", "configure the Auto profile before creating a cycle", nil)
		return
	}
	expected := *in.ExpectedProfileRevision
	in.AgentID = id
	in.Managed = true
	in.EnabledIntent = in.Enabled != nil && *in.Enabled
	if in.Enabled != nil && *in.Enabled && !p.Enabled {
		writeAPIError(w, http.StatusConflict, "profile_disabled", "enable the Auto profile before starting a cycle", nil)
		return
	}
	g, err := s.goalStore.CreateManagedCycle(in.CreateInput, in.IdempotencyKey, expected, time.Now().UTC(), false)
	if err != nil {
		writeAPIError(w, 409, "cycle_conflict", err.Error(), nil)
		return
	}
	enabled := in.Enabled != nil && *in.Enabled
	if g.SessionID == "" || g.TriggerID == "" || g.ProvisionStatus == "failed" {
		g, err = s.provisionManagedCycle(r.Context(), g, enabled)
		if err != nil {
			_, _ = s.goalStore.SetProvisionStatus(g.ID, "failed", time.Now().UTC())
			writeAPIError(w, 409, "provisioning_failed", err.Error(), map[string]any{"goal_id": g.ID})
			return
		}
	}
	writeJSON(w, 201, g)
}
