package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

type childAgentListResponse struct {
	ParentSessionID string                      `json:"parent_session_id"`
	Items           []sessionChildAgentViewJSON `json:"items"`
}

type sessionChildAgentViewJSON struct {
	ChildSessionID string `json:"child_session_id"`
	Status         string `json:"status"`
	Purpose        string   `json:"purpose"`
	AllowedTools   []string `json:"allowed_tools"`
	CreatedAt      string   `json:"created_at"`
	ExpiresAt      string `json:"expires_at"`
	TurnCount      int    `json:"turn_count"`
	MaxTurns       int    `json:"max_turns"`
}

type childAgentCancelRequest struct {
	Reason string `json:"reason"`
}

type childAgentCancelResponse struct {
	ChildSessionID string `json:"child_session_id"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status"`
}

func (s *Server) registerChildAgentRoutes() {
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/child-agents", s.handleListChildAgents)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/child-agents/{child_session_id}", s.handleGetChildAgent)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/child-agents/{child_session_id}/cancel", s.handleCancelChildAgent)
}

func (s *Server) handleListChildAgents(w http.ResponseWriter, r *http.Request) {
	parentID := strings.TrimSpace(r.PathValue("session_id"))
	items, err := s.sessions.ListChildAgents(parentID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "session_not_found", err.Error(), nil)
		return
	}
	out := childAgentListResponse{ParentSessionID: parentID, Items: make([]sessionChildAgentViewJSON, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, sessionChildAgentViewJSON{
			ChildSessionID: it.ChildSessionID,
			Status:         it.Status,
			Purpose:      it.Purpose,
			AllowedTools: append([]string(nil), it.AllowedTools...),
			CreatedAt:    it.CreatedAt.Format(timeRFC3339),
			ExpiresAt:      it.ExpiresAt.Format(timeRFC3339),
			TurnCount:      it.TurnCount,
			MaxTurns:       it.MaxTurns,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetChildAgent(w http.ResponseWriter, r *http.Request) {
	parentID := strings.TrimSpace(r.PathValue("session_id"))
	childID := strings.TrimSpace(r.PathValue("child_session_id"))
	items, err := s.sessions.ListChildAgents(parentID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "session_not_found", err.Error(), nil)
		return
	}
	for _, it := range items {
		if it.ChildSessionID == childID {
			writeJSON(w, http.StatusOK, sessionChildAgentViewJSON{
				ChildSessionID: it.ChildSessionID,
				Status:         it.Status,
				Purpose:        it.Purpose,
				AllowedTools:   append([]string(nil), it.AllowedTools...),
				CreatedAt:      it.CreatedAt.Format(timeRFC3339),
				ExpiresAt:      it.ExpiresAt.Format(timeRFC3339),
				TurnCount:      it.TurnCount,
				MaxTurns:       it.MaxTurns,
			})
			return
		}
	}
	writeAPIError(w, http.StatusNotFound, "child_agent_not_found", "child agent not found", map[string]any{"child_session_id": childID})
}

func (s *Server) handleCancelChildAgent(w http.ResponseWriter, r *http.Request) {
	parentID := strings.TrimSpace(r.PathValue("session_id"))
	childID := strings.TrimSpace(r.PathValue("child_session_id"))
	var body childAgentCancelRequest
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	prev := ""
	if items, listErr := s.sessions.ListChildAgents(parentID); listErr == nil {
		for _, it := range items {
			if it.ChildSessionID == childID {
				prev = it.Status
				break
			}
		}
	}
	res, err := s.sessions.CancelChildAgent(parentID, childID, body.Reason)
	if err != nil {
		code := "child_agent_not_found"
		if strings.Contains(err.Error(), "disabled") {
			code = "child_agents_disabled"
		}
		writeAPIError(w, http.StatusNotFound, code, err.Error(), map[string]any{"child_session_id": childID})
		return
	}
	writeJSON(w, http.StatusOK, childAgentCancelResponse{
		ChildSessionID: childID,
		Status:         string(res.Status),
		PreviousStatus: prev,
	})
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"
