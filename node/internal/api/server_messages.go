package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

type postMessageRequest struct {
	AgentID         string              `json:"agent_id"`
	RequestType     string              `json:"request_type"`
	Content         string              `json:"content"`
	ContentParts    []llm.ContentPart   `json:"content_parts,omitempty"`
	FileReferences  []llm.FileReference `json:"file_refs,omitempty"`
	UserMessageName string              `json:"user_message_name,omitempty"`
	ResumeValue     map[string]any      `json:"resume_value"`
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

	priority, err := s.sessions.EnqueueMessageWithFileReferences(r.Context(), sessionID, requestType, req.Content, req.ContentParts, req.FileReferences, req.ResumeValue, req.UserMessageName)
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
	AgentID    string `json:"agent_id"`
	Scope      string `json:"scope"`
	Cancelled  bool   `json:"cancelled"`
	TurnID     string `json:"turn_id,omitempty"`
	Generation uint64 `json:"generation,omitempty"`
	Terminal   bool   `json:"terminal"`
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
	result := s.sessions.CancelTurnWithResult(sessionID)
	writeJSON(w, http.StatusOK, cancelTurnResponse{
		AgentID:    sessionID,
		Scope:      "turn",
		Cancelled:  result.Cancelled,
		TurnID:     result.TurnID,
		Generation: result.Generation,
		Terminal:   result.Terminal,
	})
}

// handleStreams 提供按 Agent 过滤的 SSE 事件流，支持 Agent 级断点续传和
// live 模式。全局 seq 只用于线协议诊断/Last-Event-ID；过滤流的恢复游标
// 必须使用 after_agent_seq，避免其他 Agent 或 ephemeral 事件制造假洞。
func (s *Server) handleStreams(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "streaming not supported", nil)
		return
	}

	agentFilter := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	lastSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))
	lastAgentSeq := parseLastEventID(r.URL.Query().Get("after_agent_seq"))
	live := strings.TrimSpace(r.URL.Query().Get("live")) == "1"
	if !live {
		if afterRaw := strings.TrimSpace(r.URL.Query().Get("after_seq")); afterRaw != "" {
			lastSeq = parseLastEventID(afterRaw)
		}
	}
	s.logger.Info("sse subscribe",
		"agent_id", agentFilter,
		"live", live,
		"after_seq", lastSeq,
		"after_agent_seq", lastAgentSeq,
		"remote", r.RemoteAddr,
	)
	defer s.logger.Debug("sse unsubscribe", "agent_id", agentFilter, "remote", r.RemoteAddr)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	var subscription *stream.Subscription
	if live {
		subscription = s.stream.SubscribeAgentLive(agentFilter)
	} else {
		subscription = s.stream.SubscribeAgentCursor(lastSeq, lastAgentSeq, agentFilter)
	}
	events := subscription.Events
	defer s.stream.Unsubscribe(events)
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()
	if subscription.ResyncRequired {
		resync := stream.Event{
			AgentID:      agentFilter,
			Type:         "resync_required",
			EventVersion: stream.CurrentEventVersion,
			StreamEpoch:  subscription.StreamEpoch,
			Delivery:     "replayable",
			Data: map[string]any{
				"reason":           "history_truncated",
				"stream_epoch":     subscription.StreamEpoch,
				"seq":              subscription.CurrentSeq,
				"agent_seq":        subscription.CurrentAgentSeq,
				"requires_hydrate": true,
			},
		}
		if _, err := w.Write([]byte(resync.FormatSSE())); err != nil {
			return
		}
		flusher.Flush()
	}

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
