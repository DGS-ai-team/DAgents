package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

type postMessageRequest struct {
	AgentID           string              `json:"agent_id"`
	RequestType       string              `json:"request_type"`
	Content           string              `json:"content"`
	ContentParts      []llm.ContentPart   `json:"content_parts,omitempty"`
	FileReferences    []llm.FileReference `json:"file_refs,omitempty"`
	UserMessageName   string              `json:"user_message_name,omitempty"`
	ClientRequestID   string              `json:"client_request_id,omitempty"`
	ConversationEpoch uint64              `json:"conversation_epoch,omitempty"`
	ResumeValue       map[string]any      `json:"resume_value"`
}

type postMessageResponse struct {
	Accepted          bool   `json:"accepted"`
	AgentID           string `json:"agent_id"`
	Priority          string `json:"priority"`
	InputSeq          uint64 `json:"input_seq,omitempty"`
	InputID           string `json:"input_id,omitempty"`
	InputRevision     uint64 `json:"input_revision,omitempty"`
	Status            string `json:"status,omitempty"`
	ConversationEpoch uint64 `json:"conversation_epoch,omitempty"`
	Duplicate         bool   `json:"duplicate,omitempty"`
}

type messageDigestPayload struct {
	RequestType     string              `json:"request_type"`
	Content         string              `json:"content"`
	ContentParts    []llm.ContentPart   `json:"content_parts,omitempty"`
	FileReferences  []llm.FileReference `json:"file_refs,omitempty"`
	UserMessageName string              `json:"user_message_name"`
}

func digestMessageRequest(req postMessageRequest, requestType string) (string, error) {
	payload := messageDigestPayload{
		RequestType:     requestType,
		Content:         req.Content,
		ContentParts:    req.ContentParts,
		FileReferences:  req.FileReferences,
		UserMessageName: llm.NormalizeUserMessageName(req.UserMessageName),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode message request digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
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
	requestType := strings.TrimSpace(req.RequestType)
	if requestType == "" {
		requestType = "message"
	}
	if s.agents != nil {
		if rec, getErr := s.agents.Get(r.Context(), sessionID); getErr == nil && rec != nil && !rec.Archived {
			if err := s.ensureAgentRuntime(r.Context(), sessionID); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", err.Error(), map[string]any{"agent_id": sessionID})
				return
			}
		}
	}

	var priority string
	var inputSeq uint64
	var inputRevision uint64
	var receiptStatus string
	var duplicate bool
	conversationEpoch := req.ConversationEpoch
	if requestType == queue.RequestTypeMessage {
		if strings.TrimSpace(req.ClientRequestID) == "" || len(strings.TrimSpace(req.ClientRequestID)) > 128 || conversationEpoch == 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_input_receipt", "message 请求必须包含有效的 client_request_id 与 conversation_epoch", nil)
			return
		}
		digest, digestErr := digestMessageRequest(req, requestType)
		if digestErr != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_input_receipt", digestErr.Error(), nil)
			return
		}
		receipt, wasDuplicate, enqueueErr := s.sessions.EnqueueMessageWithIdempotency(r.Context(), sessionID, req.Content, req.ContentParts, req.FileReferences, req.UserMessageName, req.ClientRequestID, digest, conversationEpoch)
		priority, inputSeq, inputRevision, receiptStatus, duplicate, err = string(queue.PriorityHuman), receipt.InputSeq, receipt.InputRevision, receipt.Status, wasDuplicate, enqueueErr
	} else {
		priority, inputSeq, err = s.sessions.EnqueueMessageWithReceipt(r.Context(), sessionID, requestType, req.Content, req.ContentParts, req.FileReferences, req.ResumeValue, req.UserMessageName)
		if inputSeq > 0 {
			inputRevision = 1
			receiptStatus = "queued"
		}
	}
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
		case "result_commit_recovery_required":
			writeAPIError(w, http.StatusConflict, "result_commit_recovery_required", "本轮结果需要恢复，暂不能接收新消息", nil)
		case "invalid_input_receipt":
			writeAPIError(w, http.StatusBadRequest, "invalid_input_receipt", "client_request_id、payload 和 conversation_epoch 无效", nil)
		case "input request id conflict":
			writeAPIError(w, http.StatusConflict, "input_request_conflict", "相同 client_request_id 已用于不同的消息内容", nil)
		case "conversation changed":
			writeAPIError(w, http.StatusConflict, "conversation_changed", "对话已清空或切换，请刷新后重试", nil)
		case "input box is full":
			writeAPIError(w, http.StatusTooManyRequests, "input_queue_full", "待处理消息已达到上限，请等待当前任务完成后重试", nil)
		case "input record is too large":
			writeAPIError(w, http.StatusRequestEntityTooLarge, "input_too_large", "消息内容过大", nil)
		default:
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	inputID := ""
	if inputSeq > 0 {
		inputID = llm.InputRecordID(sessionID, inputSeq)
	}
	writeJSON(w, http.StatusOK, postMessageResponse{
		Accepted:          true,
		AgentID:           sessionID,
		Priority:          priority,
		InputSeq:          inputSeq,
		InputID:           inputID,
		InputRevision:     inputRevision,
		Status:            receiptStatus,
		ConversationEpoch: conversationEpoch,
		Duplicate:         duplicate,
	})
}

type inputReceiptResponse struct {
	Found             bool   `json:"found"`
	InputID           string `json:"input_id,omitempty"`
	InputSeq          uint64 `json:"input_seq,omitempty"`
	InputRevision     uint64 `json:"input_revision,omitempty"`
	Status            string `json:"status,omitempty"`
	ConversationEpoch uint64 `json:"conversation_epoch"`
}

func (s *Server) handleGetInputReceipt(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	requestID := strings.TrimSpace(r.URL.Query().Get("client_request_id"))
	epoch, parseErr := strconv.ParseUint(strings.TrimSpace(r.URL.Query().Get("conversation_epoch")), 10, 64)
	if agentID == "" || requestID == "" || len(requestID) > 128 || parseErr != nil || epoch == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_input_receipt", "agent_id、client_request_id 和 conversation_epoch 为必填项", nil)
		return
	}
	receipt, found, err := s.sessions.LookupInputReceipt(r.Context(), agentID, requestID, epoch)
	if err != nil {
		switch err.Error() {
		case "agent_not_found":
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": agentID})
		case "conversation changed":
			writeAPIError(w, http.StatusConflict, "conversation_changed", "对话已清空或切换，请刷新后重试", nil)
		default:
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	response := inputReceiptResponse{Found: found, ConversationEpoch: epoch}
	if found {
		response.InputID = llm.InputRecordID(agentID, receipt.InputSeq)
		response.InputSeq = receipt.InputSeq
		response.InputRevision = receipt.InputRevision
		response.Status = receipt.Status
	}
	writeJSON(w, http.StatusOK, response)
}

type mutateQueuedInputRequest struct {
	ExpectedRevision uint64              `json:"expected_revision"`
	Content          string              `json:"content"`
	ContentParts     []llm.ContentPart   `json:"content_parts,omitempty"`
	FileReferences   []llm.FileReference `json:"file_refs,omitempty"`
	UserMessageName  string              `json:"user_message_name,omitempty"`
}

type queuedInputResponse struct {
	InputID  string `json:"input_id"`
	InputSeq uint64 `json:"input_seq"`
	Revision uint64 `json:"revision"`
	Status   string `json:"status"`
}

func queuedInputPath(r *http.Request) (string, uint64, error) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	seq, err := strconv.ParseUint(strings.TrimSpace(r.PathValue("input_seq")), 10, 64)
	if agentID == "" || err != nil || seq == 0 {
		return "", 0, fmt.Errorf("invalid input identity")
	}
	return agentID, seq, nil
}

func writeQueuedInputError(w http.ResponseWriter, err error) {
	switch {
	case err.Error() == "agent_not_found":
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", nil)
	case err.Error() == "invalid_message":
		writeAPIError(w, http.StatusBadRequest, "invalid_message", "content 不能为空", nil)
	case err.Error() == "multimodal_disabled":
		writeAPIError(w, http.StatusBadRequest, "multimodal_disabled", "多模态未启用", nil)
	case err.Error() == "result_commit_recovery_required":
		writeAPIError(w, http.StatusConflict, "result_commit_recovery_required", "本轮结果需要恢复，暂不能修改待处理消息", nil)
	case errors.Is(err, session.ErrInputNotFound):
		writeAPIError(w, http.StatusNotFound, "input_not_found", "待处理消息不存在", nil)
	case errors.Is(err, session.ErrInputNotQueued):
		writeAPIError(w, http.StatusConflict, "input_already_claimed", "消息已开始处理，无法修改或撤回", nil)
	case errors.Is(err, session.ErrInputRevisionConflict):
		writeAPIError(w, http.StatusConflict, "input_revision_conflict", "待处理消息已更新，请刷新后重试", nil)
	case errors.Is(err, session.ErrInputMutationInProgress):
		writeAPIError(w, http.StatusConflict, "input_mutation_in_progress", "另一项队列变更正在保存，请稍后重试", nil)
	default:
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
	}
}

func (s *Server) handlePatchQueuedInput(w http.ResponseWriter, r *http.Request) {
	agentID, seq, err := queuedInputPath(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", err.Error(), nil)
		return
	}
	var req mutateQueuedInputRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if req.ExpectedRevision == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_revision", "expected_revision must be greater than zero", nil)
		return
	}
	record, err := s.sessions.EditQueuedMessage(agentID, seq, req.ExpectedRevision, req.Content, req.ContentParts, req.FileReferences, req.UserMessageName)
	if err != nil {
		writeQueuedInputError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, queuedInputResponse{
		InputID:  agentID + "/input/" + strconv.FormatUint(record.Seq, 10),
		InputSeq: record.Seq,
		Revision: record.Revision,
		Status:   "queued",
	})
}

func (s *Server) handleDeleteQueuedInput(w http.ResponseWriter, r *http.Request) {
	agentID, seq, err := queuedInputPath(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", err.Error(), nil)
		return
	}
	var req struct {
		ExpectedRevision uint64 `json:"expected_revision"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if req.ExpectedRevision == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_revision", "expected_revision must be greater than zero", nil)
		return
	}
	if err := s.sessions.DeleteQueuedMessage(agentID, seq, req.ExpectedRevision); err != nil {
		writeQueuedInputError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type inputQueueControlRequest struct {
	Paused           bool   `json:"paused"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

func (s *Server) handleInputQueueControl(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	if agentID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	var req inputQueueControlRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if req.ExpectedRevision == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_revision", "expected_revision must be greater than zero", nil)
		return
	}
	if s.agents != nil {
		if rec, err := s.agents.Get(r.Context(), agentID); err == nil && rec != nil && !rec.Archived {
			if err := s.ensureAgentRuntime(r.Context(), agentID); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", err.Error(), map[string]any{"agent_id": agentID})
				return
			}
		}
	}
	result, err := s.sessions.SetInputQueuePaused(agentID, req.Paused, req.ExpectedRevision)
	if err != nil {
		switch {
		case err.Error() == "agent_not_found":
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": agentID})
		case errors.Is(err, session.ErrQueueControlRevisionConflict):
			writeAPIError(w, http.StatusConflict, "queue_control_revision_conflict", "待处理队列状态已变化，请刷新后重试", nil)
		default:
			writeAPIError(w, http.StatusInternalServerError, "queue_control_failed", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type cancelTurnResponse struct {
	AgentID              string `json:"agent_id"`
	Scope                string `json:"scope"`
	Cancelled            bool   `json:"cancelled"`
	TurnID               string `json:"turn_id,omitempty"`
	Generation           uint64 `json:"generation,omitempty"`
	Terminal             bool   `json:"terminal"`
	QueuePaused          bool   `json:"queue_paused"`
	QueueControlRevision uint64 `json:"queue_control_revision"`
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
	result, queueState, err := s.sessions.StopTurnAndPauseQueue(sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "queue_pause_failed", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, cancelTurnResponse{
		AgentID:              sessionID,
		Scope:                "turn",
		Cancelled:            result.Cancelled,
		TurnID:               result.TurnID,
		Generation:           result.Generation,
		Terminal:             result.Terminal,
		QueuePaused:          queueState.Paused,
		QueueControlRevision: queueState.Revision,
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
			// Keep the comment heartbeat for generic SSE clients, and add a
			// named heartbeat so browser clients can detect half-open streams
			// after a Node restart instead of waiting forever on EventSource.
			if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "event: stream_heartbeat\ndata: {\"stream_epoch\":\"%s\"}\n\n", subscription.StreamEpoch); err != nil {
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
