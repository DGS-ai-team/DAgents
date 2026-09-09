package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
)

type maintenancePatch struct {
	Enabled  *bool   `json:"maintenance_enabled"`
	Schedule *string `json:"maintenance_schedule"`
	Timezone *string `json:"timezone"`
	Revision *int64  `json:"expected_revision"`
}

func (s *Server) maintenancePayload(agentID string) (map[string]any, error) {
	p, ok := s.goalStore.GetProfile(agentID)
	if !ok {
		return map[string]any{"configured": false, "maintenance_enabled": false, "maintenance_schedule": "", "timezone": ""}, nil
	}
	u, _ := s.goalStore.GetUsage(agentID)
	return map[string]any{"configured": true, "profile_revision": p.Revision, "maintenance_enabled": p.MaintenanceEnabled, "maintenance_schedule": p.MaintenanceSchedule, "timezone": p.Timezone, "usage": u}, nil
}

func (s *Server) handleGetAgentMaintenance(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return
	}
	if s.goalStore == nil {
		writeAPIError(w, 503, "goals_unavailable", "goal store unavailable", nil)
		return
	}
	payload, err := s.maintenancePayload(id)
	if err != nil {
		writeAPIError(w, 500, "maintenance_unavailable", err.Error(), nil)
		return
	}
	writeJSON(w, 200, payload)
}

func (s *Server) handlePatchAgentMaintenance(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return
	}
	if s.goalStore == nil {
		writeAPIError(w, 503, "goals_unavailable", "goal store unavailable", nil)
		return
	}
	p, ok := s.goalStore.GetProfile(id)
	if !ok {
		writeAPIError(w, 404, "maintenance_unconfigured", "Auto profile is not configured", nil)
		return
	}
	var in maintenancePatch
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_json", err.Error(), nil)
		return
	}
	if in.Revision == nil {
		writeAPIError(w, 400, "expected_revision_required", "expected_revision is required", nil)
		return
	}
	if *in.Revision != p.Revision {
		writeAPIError(w, 409, "profile_conflict", "maintenance configuration is stale", nil)
		return
	}
	if in.Enabled != nil {
		p.MaintenanceEnabled = *in.Enabled
	}
	if in.Schedule != nil {
		p.MaintenanceSchedule = strings.TrimSpace(*in.Schedule)
	}
	if in.Timezone != nil {
		p.Timezone = strings.TrimSpace(*in.Timezone)
	}
	saved, err := s.goalStore.SaveProfile(p, *in.Revision, time.Now().UTC())
	if err != nil {
		if !errors.Is(err, goals.ErrConflict) {
			writeAPIError(w, 400, "invalid_maintenance", err.Error(), nil)
			return
		}
		writeAPIError(w, 409, "profile_conflict", err.Error(), nil)
		return
	}
	writeJSON(w, 200, map[string]any{"profile_revision": saved.Revision, "maintenance_enabled": saved.MaintenanceEnabled, "maintenance_schedule": saved.MaintenanceSchedule, "timezone": saved.Timezone})
}

func (s *Server) handleRunAgentMaintenance(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return
	}
	if s.goalStore == nil {
		writeAPIError(w, 503, "goals_unavailable", "goal store unavailable", nil)
		return
	}
	p, ok := s.goalStore.GetProfile(id)
	if !ok || !p.MaintenanceEnabled || !p.Enabled {
		writeAPIError(w, 409, "maintenance_disabled", "daily maintenance is disabled", nil)
		return
	}
	usage, _ := s.goalStore.GetUsage(id)
	if usage.Unknown || usage.UnknownTokens > 0 {
		writeAPIError(w, 409, "recovery_required", "usage reconciliation is required", nil)
		return
	}
	if s.sessions != nil {
		_, active, _, _ := s.sessions.RuntimeInfo(id)
		if active {
			writeAPIError(w, 409, "agent_busy", "agent has an active chat turn", nil)
			return
		}
	}
	var leaseCtx context.Context = r.Context()
	if s.sessions != nil {
		ctx, release, acquired, acquireErr := s.sessions.TryAcquireMaintenanceContext(r.Context(), id)
		if acquireErr != nil {
			writeAPIError(w, 409, "maintenance_cancelled", acquireErr.Error(), nil)
			return
		}
		if !acquired {
			writeAPIError(w, 409, "agent_busy", "agent has pending chat work", nil)
			return
		}
		leaseCtx = ctx
		defer release()
	}
	for _, g := range s.goalStore.List() {
		if g.AgentID == id {
			for _, run := range s.goalStore.Runs(g.ID) {
				if run.FinishedAt == nil {
					writeAPIError(w, 409, "agent_busy", "agent has an active business run", nil)
					return
				}
			}
		}
	}
	if s.store == nil || s.agents == nil {
		writeAPIError(w, 503, "maintenance_unavailable", "maintenance persistence unavailable", nil)
		return
	}
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil || rec == nil {
		writeAPIError(w, 404, "agent_not_found", "agent not found", nil)
		return
	}
	ms, err := s.openAgentMemoryService(id, rec)
	if err != nil || ms == nil {
		writeAPIError(w, 500, "maintenance_unavailable", fmt.Sprint(err), nil)
		return
	}
	defer ms.Close()
	var extractor memory.MaintenanceUsageExtractor = s.maintenanceExtractor
	if extractor == nil {
		client, _, resolveErr := s.llmClientForAgent(leaseCtx, rec, id)
		if resolveErr != nil {
			writeAPIError(w, 503, "llm_unavailable", resolveErr.Error(), nil)
			return
		}
		extractor = memory.NewLLMCandidateExtractor(client, 8, 24000)
	}
	cursor, err := ms.GetMaintenanceCursor(r.Context())
	if err != nil {
		writeAPIError(w, 500, "maintenance_unavailable", err.Error(), nil)
		return
	}
	runner := &memory.MaintenanceRunner{Source: maintenanceSource{store: s.store}, Extractor: extractor, Memory: ms, Usage: s.goalStore}
	next, err := runner.RunOnce(leaseCtx, id, cursor)
	if err != nil {
		writeAPIError(w, 409, "maintenance_failed", err.Error(), map[string]any{"sequence": next})
		return
	}
	u, _ := s.goalStore.GetUsage(id)
	writeJSON(w, 200, map[string]any{"status": "completed", "sequence": next, "usage": u})
}
