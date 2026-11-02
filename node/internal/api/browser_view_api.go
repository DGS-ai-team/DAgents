package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
)

func (s *Server) registerBrowserViewRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/browser/view", s.handleAgentBrowserView)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/browser/control", s.handleAgentBrowserControl)
}

// handleAgentBrowserView exposes only screenshot and tab metadata for the
// browser session owned by this Agent. It does not ensure/start an Agent or
// execute page code.
func (s *Server) handleAgentBrowserView(w http.ResponseWriter, r *http.Request) {
	s.handleAgentBrowserRequest(w, r, false)
}

func (s *Server) handleAgentBrowserControl(w http.ResponseWriter, r *http.Request) {
	s.handleAgentBrowserRequest(w, r, true)
}

func (s *Server) handleAgentBrowserRequest(w http.ResponseWriter, r *http.Request, control bool) {
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
	var result browser.ToolResult
	if control {
		var input struct {
			Command string `json:"command"`
			PageID  string `json:"page_id"`
			URL     string `json:"url"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_action", "无效的浏览器操作", nil)
			return
		}
		pageID = strings.TrimSpace(input.PageID)
		switch input.Command {
		case "navigate":
			u, parseErr := url.Parse(strings.TrimSpace(input.URL))
			if parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
				writeAPIError(w, http.StatusBadRequest, "invalid_url", "请输入有效的 http 或 https 网址", nil)
				return
			}
		case "new":
		case "back", "forward", "reload", "close":
			if pageID == "" {
				writeAPIError(w, http.StatusBadRequest, "invalid_action", "请选择一个页面", nil)
				return
			}
		default:
			writeAPIError(w, http.StatusBadRequest, "invalid_action", "不支持的浏览器操作", nil)
			return
		}
		result, err = manager.Control(r.Context(), agentID, input.Command, pageID, strings.TrimSpace(input.URL))
	} else {
		result, err = manager.View(r.Context(), agentID, pageID)
	}
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
		case "invalid_action", "navigation_blocked":
			status, code = http.StatusBadRequest, result.ErrorCode
		case "action_timeout":
			status, code = http.StatusGatewayTimeout, "browser_navigation_timeout"
		}
		writeAPIError(w, status, code, result.Error, map[string]any{"agent_id": agentID, "page_id": pageID})
		return
	}
	writeJSON(w, http.StatusOK, result.Detail)
}
