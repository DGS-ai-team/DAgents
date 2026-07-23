package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func (s *Server) registerToolCallControlRoutes() {
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/tool-calls/{tool_call_id}/cancel", s.handleAgentToolCallCancel)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/tool-calls/{tool_call_id}/background", s.handleAgentToolCallBackground)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/tool-jobs", s.handleAgentToolJobs)
}

func (s *Server) agentToolsRegistry(agentID string) (*tools.Registry, string, bool) {
	id := strings.TrimSpace(agentID)
	if id == "" || s.sessions == nil {
		return nil, id, false
	}
	reg := s.sessions.SessionTools(id)
	if reg == nil {
		reg = s.sessions.DefaultTools()
	}
	return reg, id, reg != nil
}

func (s *Server) handleAgentToolCallCancel(w http.ResponseWriter, r *http.Request) {
	reg, agentID, ok := s.agentToolsRegistry(r.PathValue("agent_id"))
	if !ok {
		writeAPIError(w, http.StatusNotFound, "agent_runtime_missing", "agent runtime 未就绪", map[string]any{"agent_id": agentID})
		return
	}
	toolCallID := strings.TrimSpace(r.PathValue("tool_call_id"))
	if toolCallID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_tool_call", "tool_call_id is required", nil)
		return
	}
	if err := reg.CancelSyncBash(agentID, toolCallID); err != nil {
		writeSyncShellControlError(w, err, agentID, toolCallID)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"agent_id":     agentID,
		"tool_call_id": toolCallID,
		"action":       "cancel",
	})
}

func (s *Server) handleAgentToolCallBackground(w http.ResponseWriter, r *http.Request) {
	reg, agentID, ok := s.agentToolsRegistry(r.PathValue("agent_id"))
	if !ok {
		writeAPIError(w, http.StatusNotFound, "agent_runtime_missing", "agent runtime 未就绪", map[string]any{"agent_id": agentID})
		return
	}
	toolCallID := strings.TrimSpace(r.PathValue("tool_call_id"))
	if toolCallID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_tool_call", "tool_call_id is required", nil)
		return
	}
	if err := reg.BackgroundSyncBash(agentID, toolCallID); err != nil {
		writeSyncShellControlError(w, err, agentID, toolCallID)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"agent_id":     agentID,
		"tool_call_id": toolCallID,
		"action":       "background",
	})
}

func (s *Server) handleAgentToolJobs(w http.ResponseWriter, r *http.Request) {
	reg, agentID, ok := s.agentToolsRegistry(r.PathValue("agent_id"))
	if !ok {
		writeAPIError(w, http.StatusNotFound, "agent_runtime_missing", "agent runtime 未就绪", map[string]any{"agent_id": agentID})
		return
	}
	counts := reg.SessionToolJobCounts(agentID)
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":            agentID,
		"running":             counts.Running,
		"background":          counts.Background,
		"running_call_ids":    counts.RunningCallIDs,
		"background_call_ids": counts.BackgroundCallIDs,
	})
}

func writeSyncShellControlError(w http.ResponseWriter, err error, agentID, toolCallID string) {
	if errors.Is(err, tools.ErrSyncShellNotFound) {
		writeAPIError(w, http.StatusConflict, "tool_call_not_running", "可控制的 bash 工具调用不存在或已结束", map[string]any{
			"agent_id":     agentID,
			"tool_call_id": toolCallID,
		})
		return
	}
	if errors.Is(err, tools.ErrSyncShellNotBash) {
		writeAPIError(w, http.StatusBadRequest, "unsupported_tool", "仅 bash_run 支持终止/转后台", map[string]any{
			"agent_id":     agentID,
			"tool_call_id": toolCallID,
		})
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "tool_control_failed", err.Error(), map[string]any{
		"agent_id":     agentID,
		"tool_call_id": toolCallID,
	})
}
