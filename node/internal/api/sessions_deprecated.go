package api

import "net/http"

// registerLegacySessionsGone 将已下线的 /v1/sessions* 固定返回 410（对齐 /v1/policy）。
func (s *Server) registerLegacySessionsGone() {
	gone := s.handleLegacySessionsGone
	s.mux.HandleFunc("POST /v1/sessions", gone)
	s.mux.HandleFunc("GET /v1/sessions", gone)
	s.mux.HandleFunc("DELETE /v1/sessions/{session_id}", gone)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/clear-context", gone)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/compress", gone)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/context", gone)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/hydrate", gone)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/ack", gone)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/cancel", gone)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/skills", gone)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/skills/load", gone)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/skills/unload", gone)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/child-agents", gone)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/child-agents/{child_session_id}", gone)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/child-agents/{child_session_id}/cancel", gone)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/media/{media_id}", gone)
}

func (s *Server) handleLegacySessionsGone(w http.ResponseWriter, _ *http.Request) {
	writeAPIError(w, http.StatusGone, "sessions_moved",
		"/v1/sessions* 已下线；请改用 /v1/agents/{agent_id}/...", nil)
}
