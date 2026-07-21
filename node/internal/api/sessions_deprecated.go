package api

import "net/http"

// registerSessionRoutes 注册过渡期 /v1/sessions*（对外契约以 /v1/agents 为准）。
// handlers 亦被 agent 路径别名复用。
func (s *Server) registerSessionRoutes() {
	s.mux.HandleFunc("POST /v1/sessions", s.withSessionsDeprecated(s.handleCreateSession))
	s.mux.HandleFunc("GET /v1/sessions", s.withSessionsDeprecated(s.handleListSessions))
	s.mux.HandleFunc("DELETE /v1/sessions/{session_id}", s.withSessionsDeprecated(s.handleDeleteSession))
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/clear-context", s.withSessionsDeprecated(s.handleClearContext))
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/compress", s.withSessionsDeprecated(s.handleCompressContext))
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/context", s.withSessionsDeprecated(s.handleSessionContext))
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/hydrate", s.withSessionsDeprecated(s.handleSessionHydrate))
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/ack", s.withSessionsDeprecated(s.handleSessionAck))
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/cancel", s.withSessionsDeprecated(s.handleCancelSession))
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/skills", s.withSessionsDeprecated(s.handleListSessionSkills))
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/skills/load", s.withSessionsDeprecated(s.handleLoadSessionSkill))
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/skills/unload", s.withSessionsDeprecated(s.handleUnloadSessionSkill))
}

func (s *Server) withSessionsDeprecated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Link", `</v1/agents/{agent_id}>; rel="successor-version"`)
		w.Header().Set("Warning", `299 - " /v1/sessions* is deprecated; use /v1/agents/{agent_id} "`)
		next(w, r)
	}
}
