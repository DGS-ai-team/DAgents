package api

import (
	"net/http"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
)

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
