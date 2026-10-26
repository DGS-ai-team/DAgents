package api

import (
	"net/http"
	"strings"
)

func (s *Server) registerBrowserViewRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/browser/view", s.handleAgentBrowserView)
}

// handleAgentBrowserView exposes only screenshot and tab metadata for the
// browser session owned by this Agent. It does not ensure/start an Agent or
// execute page code.
func (s *Server) handleAgentBrowserView(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	if agentID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	rec, err := s.agents.Get(r.Context(), agentID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
		return
	}
	if rec == nil || rec.Archived {
		s.writeAgentNotFound(w, agentID)
		return
	}
	manager := s.browserManager()
	if manager == nil || !manager.Enabled() {
		writeAPIError(w, http.StatusServiceUnavailable, "browser_disabled", "浏览器能力未配置或未启用", nil)
		return
	}
	pageID := strings.TrimSpace(r.URL.Query().Get("page_id"))
	result, err := manager.View(r.Context(), agentID, pageID)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "browser_view_failed", err.Error(), map[string]any{"agent_id": agentID})
		return
	}
	if !result.OK {
		status, code := http.StatusBadGateway, "browser_view_failed"
		switch result.ErrorCode {
		case "browser_busy":
			status, code = http.StatusLocked, "browser_busy"
		case "browser_not_started":
			status, code = http.StatusConflict, "browser_not_started"
		case "page_not_found", "page_closed":
			status, code = http.StatusNotFound, "browser_page_not_found"
		case "image_too_large":
			status, code = http.StatusRequestEntityTooLarge, "browser_image_too_large"
		case "view_timeout":
			status, code = http.StatusGatewayTimeout, "browser_view_timeout"
		}
		writeAPIError(w, status, code, result.Error, map[string]any{"agent_id": agentID, "page_id": pageID})
		return
	}
	writeJSON(w, http.StatusOK, result.Detail)
}
