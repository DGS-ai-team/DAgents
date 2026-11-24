package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

type conversationResubmitTargetResponse struct {
	AgentID               string                    `json:"agent_id"`
	ConversationID        string                    `json:"conversation_id"`
	ConversationRevision  uint64                    `json:"conversation_revision"`
	ConversationEpoch     uint64                    `json:"conversation_epoch"`
	RoundID               string                    `json:"round_id"`
	RevisionID            string                    `json:"revision_id"`
	PreCheckpointRevision uint64                    `json:"pre_checkpoint_revision"`
	Input                 conversationResubmitInput `json:"input"`
}

type conversationResubmitInput struct {
	Content         string              `json:"content,omitempty"`
	ContentParts    []llm.ContentPart   `json:"content_parts,omitempty"`
	FileReferences  []llm.FileReference `json:"file_refs,omitempty"`
	UserMessageName string              `json:"user_message_name,omitempty"`
}

type conversationResubmitRequest struct {
	ExpectedSelectedRevisionID   string              `json:"expected_selected_revision_id"`
	ExpectedConversationRevision uint64              `json:"expected_conversation_revision"`
	PreCheckpointRevision        uint64              `json:"pre_checkpoint_revision"`
	ConversationEpoch            uint64              `json:"conversation_epoch"`
	ClientRequestID              string              `json:"client_request_id"`
	Content                      string              `json:"content"`
	ContentParts                 []llm.ContentPart   `json:"content_parts,omitempty"`
	FileReferences               []llm.FileReference `json:"file_refs,omitempty"`
	UserMessageName              string              `json:"user_message_name,omitempty"`
}

type conversationResubmitDigest struct {
	RequestType                  string              `json:"request_type"`
	RoundID                      string              `json:"round_id"`
	ExpectedSelectedRevisionID   string              `json:"expected_selected_revision_id"`
	ExpectedConversationRevision uint64              `json:"expected_conversation_revision"`
	PreCheckpointRevision        uint64              `json:"pre_checkpoint_revision"`
	Content                      string              `json:"content"`
	ContentParts                 []llm.ContentPart   `json:"content_parts,omitempty"`
	FileReferences               []llm.FileReference `json:"file_refs,omitempty"`
	UserMessageName              string              `json:"user_message_name"`
}

func digestConversationResubmit(roundID string, req conversationResubmitRequest) (string, error) {
	payload := conversationResubmitDigest{
		RequestType: "resubmit", RoundID: strings.TrimSpace(roundID),
		ExpectedSelectedRevisionID:   strings.TrimSpace(req.ExpectedSelectedRevisionID),
		ExpectedConversationRevision: req.ExpectedConversationRevision,
		PreCheckpointRevision:        req.PreCheckpointRevision, Content: req.Content,
		ContentParts: req.ContentParts, FileReferences: req.FileReferences,
		UserMessageName: llm.NormalizeUserMessageName(req.UserMessageName),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode resubmit request digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func conversationResubmitPath(r *http.Request) (string, string, string, error) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	conversationID := strings.TrimSpace(r.PathValue("conversation_id"))
	roundID := strings.TrimSpace(r.PathValue("round_id"))
	if agentID == "" || conversationID == "" || roundID == "" || agentID != conversationID {
		return "", "", "", fmt.Errorf("invalid conversation resubmit path")
	}
	return agentID, conversationID, roundID, nil
}

func (s *Server) handleConversationResubmitTarget(w http.ResponseWriter, r *http.Request) {
	agentID, conversationID, roundID, err := conversationResubmitPath(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_conversation", err.Error(), nil)
		return
	}
	revisionID := strings.TrimSpace(r.URL.Query().Get("revision_id"))
	if revisionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_revision", "revision_id is required", nil)
		return
	}
	target, epoch, err := s.sessions.LoadConversationResubmitTarget(r.Context(), conversationID, roundID, revisionID)
	if err != nil {
		writeConversationResubmitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationResubmitTargetResponse{
		AgentID: agentID, ConversationID: target.ConversationID,
		ConversationRevision: target.ConversationRevision, ConversationEpoch: epoch,
		RoundID: target.RoundID, RevisionID: target.RevisionID,
		PreCheckpointRevision: target.PreCheckpointRevision,
		Input: conversationResubmitInput{
			Content: target.Input.Content, ContentParts: target.Input.ContentParts,
			FileReferences: target.Input.FileReferences, UserMessageName: target.Input.Name,
		},
	})
}

func (s *Server) handleConversationResubmit(w http.ResponseWriter, r *http.Request) {
	agentID, conversationID, roundID, err := conversationResubmitPath(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_conversation", err.Error(), nil)
		return
	}
	var req conversationResubmitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if req.ConversationEpoch == 0 || req.ExpectedConversationRevision == 0 || strings.TrimSpace(req.ExpectedSelectedRevisionID) == "" ||
		strings.TrimSpace(req.ClientRequestID) == "" || len(strings.TrimSpace(req.ClientRequestID)) > 128 {
		writeAPIError(w, http.StatusBadRequest, "invalid_resubmit", "conversation epoch, target revision, and client_request_id are required", nil)
		return
	}
	digest, err := digestConversationResubmit(roundID, req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_resubmit", err.Error(), nil)
		return
	}
	resubmit := queue.ConversationResubmitRequest{
		RoundID:                      roundID,
		ExpectedSelectedRevisionID:   strings.TrimSpace(req.ExpectedSelectedRevisionID),
		ExpectedConversationRevision: req.ExpectedConversationRevision,
		PreCheckpointRevision:        req.PreCheckpointRevision,
	}
	receipt, duplicate, err := s.sessions.EnqueueConversationResubmit(
		r.Context(), conversationID, resubmit, req.Content, req.ContentParts, req.FileReferences,
		req.UserMessageName, req.ClientRequestID, digest, req.ConversationEpoch,
	)
	if err != nil {
		writeConversationResubmitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, postMessageResponse{
		Accepted: true, AgentID: agentID, Priority: string(queue.PriorityHuman),
		InputSeq: receipt.InputSeq, InputID: llm.InputRecordID(agentID, receipt.InputSeq),
		InputRevision: receipt.InputRevision, Status: receipt.Status,
		ConversationEpoch: req.ConversationEpoch, Duplicate: duplicate,
	})
}

func writeConversationResubmitError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrConversationResubmitConflict):
		writeAPIError(w, http.StatusConflict, "resubmit_target_changed", "可重新发送的目标已变化或会话正忙，请刷新历史后重试", nil)
	case errors.Is(err, session.ErrInputConversationChanged):
		writeAPIError(w, http.StatusConflict, "conversation_changed", "对话已清空或切换，请刷新后重试", nil)
	case err.Error() == "agent_not_found":
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在或尚未加载", nil)
	case err.Error() == "invalid_message":
		writeAPIError(w, http.StatusBadRequest, "invalid_message", "content 不能为空", nil)
	case err.Error() == "multimodal_disabled":
		writeAPIError(w, http.StatusBadRequest, "multimodal_disabled", "多模态未启用", nil)
	case err.Error() == "result_commit_recovery_required":
		writeAPIError(w, http.StatusConflict, "result_commit_recovery_required", "本轮结果需要恢复，暂不能重新发送", nil)
	case errors.Is(err, session.ErrInputRequestConflict):
		writeAPIError(w, http.StatusConflict, "input_request_conflict", "相同 client_request_id 已用于不同的消息内容", nil)
	case errors.Is(err, session.ErrInputRecordTooBig):
		writeAPIError(w, http.StatusRequestEntityTooLarge, "input_too_large", "消息内容过大", nil)
	default:
		writeAPIError(w, http.StatusInternalServerError, "resubmit_failed", "重新发送消息失败", nil)
	}
}
