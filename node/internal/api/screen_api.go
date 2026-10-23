package api

import (
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/screen"
)

func (s *Server) registerScreenRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/screen/status", s.handleAgentScreenStatus)
}

func (s *Server) handleAgentScreenStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if s.agents != nil {
		rec, err := s.agents.Get(r.Context(), id)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
			return
		}
		if rec == nil || rec.Archived {
			s.writeAgentNotFound(w, id)
			return
		}
	}
	st := screen.Default().Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": id,
		"host":     st,
	})
}
