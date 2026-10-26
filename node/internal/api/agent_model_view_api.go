package api

import (
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/setup"
)

type agentModelViewResponse struct {
	Agent          agentView                  `json:"agent"`
	LLMSettings    llm.LLMSettingsView        `json:"llm_settings"`
	ProfileCatalog []setup.LLMProfileSettings `json:"profile_catalog"`
}

// handleGetAgentModelView returns the effective per-Agent model and the
// selectable profile metadata together, so Desktop does not need to fetch and
// reconcile agent, runtime, and setup configuration separately.
func (s *Server) handleGetAgentModelView(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	s.llmRefMu.RLock()
	defer s.llmRefMu.RUnlock()
	id := strings.TrimSpace(r.PathValue("agent_id"))
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
		return
	}
	if rec == nil || rec.Archived {
		s.writeAgentNotFound(w, id)
		return
	}
	snapshot, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_invalid", err.Error(), nil)
		return
	}
	view := s.llmSettingsView()
	profileID := strings.TrimSpace(agentruntime.LLMActiveFromDefaults(snapshot))
	if profileID == "" && s.cfg != nil {
		profileID = s.cfg.LLM.ActiveProfileID()
	}
	view.ActiveProfile = profileID
	setupView := setup.ViewFromConfig(s.cfg)
	if profileID != "" {
		for _, profile := range setupView.LLM.Profiles {
			if profile.ID != profileID {
				continue
			}
			view.Provider = profile.Provider
			view.Model = profile.Model
			view.Mock = profile.Mock
			view.MultimodalEnabled = profile.MultimodalEnabled
			view.Thinking = profile.Thinking
			view.ReasoningEffort = profile.ReasoningEffort
			view.ThinkingSupported = llm.ThinkingSupported(profile.Provider)
			view.ThinkingControl, view.ThinkingLabel, view.ThinkingSecondaryLabel = llm.ThinkingControlMetadata(profile.Provider, profile.Model)
			break
		}
	}
	writeJSON(w, http.StatusOK, agentModelViewResponse{
		Agent:          agentViewFromRecord(*rec),
		LLMSettings:    view,
		ProfileCatalog: setupView.LLM.Profiles,
	})
}
