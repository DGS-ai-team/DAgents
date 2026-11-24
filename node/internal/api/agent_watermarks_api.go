package api

import (
	"net/http"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
)

type agentWatermarksResponse struct {
	StreamEpoch string                          `json:"stream_epoch"`
	Agents      []session.ConversationWatermark `json:"agents"`
}

// handleAgentWatermarks returns content-free target revisions for every
// stored or already-loaded conversation. It must never create a runtime or
// read transcript/checkpoint payloads.
func (s *Server) handleAgentWatermarks(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "sessions_unavailable", "session manager is not configured", nil)
		return
	}
	items, err := s.sessions.ListConversationWatermarks(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	if items == nil {
		items = []session.ConversationWatermark{}
	}
	epoch := ""
	if s.stream != nil {
		epoch = s.stream.Epoch()
	}
	writeJSON(w, http.StatusOK, agentWatermarksResponse{StreamEpoch: epoch, Agents: items})
}
