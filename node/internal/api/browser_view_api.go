package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
)

func (s *Server) registerBrowserViewRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/browser/view", s.handleAgentBrowserView)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/browser/action", s.handleAgentBrowserAction)
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
	if !s.requireBrowserAgent(w, r, agentID) {
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

type agentBrowserActionInput struct {
	Action string `json:"action"`
	PageID string `json:"page_id,omitempty"`
	URL    string `json:"url,omitempty"`
}

func (s *Server) handleAgentBrowserAction(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	if agentID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if !s.requireBrowserAgent(w, r, agentID) {
		return
	}
	manager := s.browserManager()
	if manager == nil || !manager.Enabled() {
		writeAPIError(w, http.StatusServiceUnavailable, "browser_disabled", "浏览器能力未配置或未启用", nil)
		return
	}
	var in agentBrowserActionInput
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	in.Action = strings.TrimSpace(in.Action)
	in.PageID = strings.TrimSpace(in.PageID)
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	var result browser.ToolResult
	var err error
	switch in.Action {
	case "start":
		headed := false // Desktop renders the same Agent-owned session in its resource surface.
		result, err = manager.Start(ctx, agentID, &headed, 1440, 900)
	case "new_tab":
		result, err = manager.Control(ctx, agentID, "", browser.Action{Op: "new_page"})
	case "navigate":
		normalized, normalizeErr := normalizeDesktopBrowserURL(in.URL)
		if normalizeErr != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_browser_url", normalizeErr.Error(), nil)
			return
		}
		result, err = manager.Control(ctx, agentID, in.PageID, browser.Action{Op: "navigate", Params: map[string]any{"url": normalized}})
	case "back", "forward", "reload":
		result, err = manager.Control(ctx, agentID, in.PageID, browser.Action{Op: in.Action})
	case "close_tab":
		tabs, tabsErr := manager.Control(ctx, agentID, "", browser.Action{Op: "tabs"})
		if tabsErr != nil || !tabs.OK {
			writeBrowserActionError(w, agentID, tabs, tabsErr)
			return
		}
		if browserTabCount(tabs) <= 1 {
			writeAPIError(w, http.StatusConflict, "browser_last_tab", "至少保留一个浏览器标签页", nil)
			return
		}
		result, err = manager.Control(ctx, agentID, in.PageID, browser.Action{Op: "close_page"})
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_browser_action", "不支持此浏览器操作", nil)
		return
	}
	if err != nil || !result.OK {
		writeBrowserActionError(w, agentID, result, err)
		return
	}
	pageID := in.PageID
	if in.Action == "start" || in.Action == "new_tab" || in.Action == "close_tab" {
		pageID = "" // The sidecar's most recently opened page becomes active.
	}
	view, err := manager.View(ctx, agentID, pageID)
	if err != nil || !view.OK {
		writeBrowserActionError(w, agentID, view, err)
		return
	}
	writeJSON(w, http.StatusOK, view.Detail)
}

func (s *Server) requireBrowserAgent(w http.ResponseWriter, r *http.Request, agentID string) bool {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return false
	}
	rec, err := s.agents.Get(r.Context(), agentID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
		return false
	}
	if rec == nil || rec.Archived {
		s.writeAgentNotFound(w, agentID)
		return false
	}
	return true
}

func normalizeDesktopBrowserURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 4096 {
		return "", fmt.Errorf("请输入有效的网址")
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("仅支持 http 或 https 网页地址")
	}
	return parsed.String(), nil
}

func browserTabCount(result browser.ToolResult) int {
	actions, _ := result.Detail["action_results"].([]any)
	if len(actions) == 0 {
		return 0
	}
	first, _ := actions[0].(map[string]any)
	tabs, _ := first["data"].([]any)
	return len(tabs)
}

func writeBrowserActionError(w http.ResponseWriter, agentID string, result browser.ToolResult, err error) {
	message, code := "浏览器操作失败", result.ErrorCode
	if err != nil {
		message = err.Error()
	} else if strings.TrimSpace(result.Error) != "" {
		message = result.Error
	}
	status := http.StatusBadGateway
	switch code {
	case "browser_busy":
		status = http.StatusLocked
	case "browser_not_started":
		status = http.StatusConflict
	case "page_not_found", "page_closed":
		status = http.StatusNotFound
	case "browser_disabled":
		status = http.StatusServiceUnavailable
	case "invalid_action", "navigation_blocked":
		status = http.StatusBadRequest
	case "last_page":
		status = http.StatusConflict
	}
	writeAPIError(w, status, codeOr(code, "browser_action_failed"), message, map[string]any{"agent_id": agentID})
}

func codeOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
