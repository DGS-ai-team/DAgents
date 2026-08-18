package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type postMessageRequest struct {
	AgentID         string            `json:"agent_id"`
	RequestType     string            `json:"request_type"`
	Content         string            `json:"content"`
	ContentParts    []llm.ContentPart `json:"content_parts,omitempty"`
	UserMessageName string            `json:"user_message_name,omitempty"`
	ResumeValue     map[string]any    `json:"resume_value"`
}

type postMessageResponse struct {
	Accepted bool   `json:"accepted"`
	AgentID  string `json:"agent_id"`
	Priority string `json:"priority"`
}

func resolveAgentID(agentID string) (string, error) {
	aid := strings.TrimSpace(agentID)
	if aid == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	return aid, nil
}

// handlePostMessage 只负责请求校验、Agent runtime 确保和消息入队。
// LLM Turn 与工具执行由 session.Manager 异步完成，结果通过 /v1/streams 推送。
func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	var req postMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	sessionID, err := resolveAgentID(req.AgentID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", err.Error(), nil)
		return
	}
	if s.agents != nil {
		if rec, getErr := s.agents.Get(r.Context(), sessionID); getErr == nil && rec != nil && !rec.Archived {
			if s.retireRemoteStubIfNeeded(r.Context(), w, rec) {
				return
			}
			if err := s.ensureAgentRuntime(r.Context(), sessionID); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", err.Error(), map[string]any{"agent_id": sessionID})
				return
			}
		}
	}
	requestType := strings.TrimSpace(req.RequestType)
	if requestType == "" {
		requestType = "message"
	}

	priority, err := s.sessions.EnqueueMessage(r.Context(), sessionID, requestType, req.Content, req.ContentParts, req.ResumeValue, req.UserMessageName)
	if err != nil {
		switch err.Error() {
		case "agent_not_found":
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		case "invalid_message":
			writeAPIError(w, http.StatusBadRequest, "invalid_message", "content 不能为空", nil)
		case "multimodal_disabled":
			writeAPIError(w, http.StatusBadRequest, "multimodal_disabled", "多模态未启用（config multimodal.enabled）", nil)
		case "invalid_request_type":
			writeAPIError(w, http.StatusBadRequest, "invalid_request_type", "不支持的 request_type", nil)
		case "no_pending_hitl":
			writeAPIError(w, http.StatusConflict, "no_pending_hitl", "当前无等待中的 HITL", nil)
		default:
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, postMessageResponse{
		Accepted: true,
		AgentID:  sessionID,
		Priority: priority,
	})
}

type cancelTurnResponse struct {
	AgentID   string `json:"agent_id"`
	Cancelled bool   `json:"cancelled"`
}

func (s *Server) handleAgentCancelImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if s.sessions.Get(sessionID) == nil {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		return
	}
	cancelled := s.sessions.CancelTurn(sessionID)
	writeJSON(w, http.StatusOK, cancelTurnResponse{
		AgentID:   sessionID,
		Cancelled: cancelled,
	})
}

// handleStreams 提供按 Agent 过滤的 SSE 事件流，支持断点续传和 live 模式。
func (s *Server) handleStreams(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "streaming not supported", nil)
		return
	}

	agentFilter := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if agentFilter != "" && s.agents != nil {
		if rec, err := s.agents.Get(r.Context(), agentFilter); err == nil && rec != nil && !rec.Archived {
			if s.retireRemoteStubIfNeeded(r.Context(), w, rec) {
				return
			}
		}
	}
	lastSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))
	live := strings.TrimSpace(r.URL.Query().Get("live")) == "1"
	if live {
		lastSeq = s.stream.CurrentSeq()
	} else if afterRaw := strings.TrimSpace(r.URL.Query().Get("after_seq")); afterRaw != "" {
		lastSeq = parseLastEventID(afterRaw)
	}
	s.logger.Info("sse subscribe",
		"agent_id", agentFilter,
		"live", live,
		"after_seq", lastSeq,
		"remote", r.RemoteAddr,
	)
	defer s.logger.Debug("sse unsubscribe", "agent_id", agentFilter, "remote", r.RemoteAddr)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events := s.stream.SubscribeAgent(lastSeq, agentFilter)
	defer s.stream.Unsubscribe(events)
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			if _, err := w.Write([]byte(ev.FormatSSE())); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
