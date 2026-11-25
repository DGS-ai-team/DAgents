package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

type conversationRoundItemsResponse struct {
	AgentID              string                    `json:"agent_id"`
	ConversationID       string                    `json:"conversation_id"`
	ConversationRevision uint64                    `json:"conversation_revision"`
	RoundID              string                    `json:"round_id"`
	RevisionID           string                    `json:"revision_id"`
	RevisionStatus       string                    `json:"revision_status"`
	Selected             bool                      `json:"selected"`
	Items                []session.TranscriptEntry `json:"items"`
	HasMore              bool                      `json:"has_more"`
	NextAfterItemID      string                    `json:"next_after_item_id,omitempty"`
}

type conversationLegacyItemsResponse struct {
	AgentID              string                    `json:"agent_id"`
	ConversationID       string                    `json:"conversation_id"`
	ConversationRevision uint64                    `json:"conversation_revision"`
	HistoryCompleteness  string                    `json:"history_completeness"`
	ItemCount            int64                     `json:"item_count"`
	Items                []session.TranscriptEntry `json:"items"`
	HasMore              bool                      `json:"has_more"`
	NextAfterItemID      string                    `json:"next_after_item_id,omitempty"`
}

type conversationRoundRevisionsResponse struct {
	AgentID              string                              `json:"agent_id"`
	ConversationID       string                              `json:"conversation_id"`
	ConversationRevision uint64                              `json:"conversation_revision"`
	RoundID              string                              `json:"round_id"`
	SelectedRevisionID   string                              `json:"selected_revision_id"`
	Revisions            []store.ConversationRevisionSummary `json:"revisions"`
}

type conversationToolStepView struct {
	ToolCallID  string                  `json:"tool_call_id"`
	CallEntry   session.TranscriptEntry `json:"call_entry"`
	ResultEntry session.TranscriptEntry `json:"result_entry,omitempty"`
}

type conversationToolStepsResponse struct {
	AgentID              string                     `json:"agent_id"`
	ConversationID       string                     `json:"conversation_id"`
	ConversationRevision uint64                     `json:"conversation_revision"`
	RoundID              string                     `json:"round_id"`
	RevisionID           string                     `json:"revision_id"`
	RevisionStatus       string                     `json:"revision_status"`
	Selected             bool                       `json:"selected"`
	SourceItemID         string                     `json:"source_item_id"`
	TotalCount           int                        `json:"total_count"`
	Steps                []conversationToolStepView `json:"steps"`
	HasMore              bool                       `json:"has_more"`
	NextAfterToolCallID  string                     `json:"next_after_tool_call_id,omitempty"`
}

func (s *Server) handleConversationRoundPage(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	conversationID := strings.TrimSpace(r.PathValue("conversation_id"))
	if agentID == "" || conversationID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_conversation", "agent_id and conversation_id are required", nil)
		return
	}
	if agentID != conversationID {
		writeAPIError(w, http.StatusNotFound, "conversation_not_found", "对话不存在", nil)
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "history_unavailable", "历史存储当前不可用", nil)
		return
	}
	limit, err := historyPageLimit(r, 10, 100)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_history_limit", err.Error(), nil)
		return
	}
	beforeRoundID := strings.TrimSpace(r.URL.Query().Get("before_round_id"))
	page, err := s.store.LoadConversationRoundPage(r.Context(), conversationID, beforeRoundID, limit)
	if err != nil {
		writeConversationHistoryError(w, err)
		return
	}
	page.AgentID = agentID
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleConversationRoundItems(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	conversationID := strings.TrimSpace(r.PathValue("conversation_id"))
	roundID := strings.TrimSpace(r.PathValue("round_id"))
	if agentID == "" || conversationID == "" || roundID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_conversation", "agent_id, conversation_id and round_id are required", nil)
		return
	}
	if agentID != conversationID {
		writeAPIError(w, http.StatusNotFound, "conversation_not_found", "对话不存在", nil)
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "history_unavailable", "历史存储当前不可用", nil)
		return
	}
	limit, err := historyPageLimit(r, 50, 200)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_history_limit", err.Error(), nil)
		return
	}
	page, err := s.store.LoadConversationRoundItems(r.Context(), conversationID, roundID, r.URL.Query().Get("revision_id"), r.URL.Query().Get("after_item_id"), limit)
	if err != nil {
		writeConversationHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationRoundItemsResponse{
		AgentID: agentID, ConversationID: page.ConversationID,
		ConversationRevision: page.ConversationRevision, RoundID: page.RoundID,
		RevisionID: page.RevisionID, RevisionStatus: page.RevisionStatus,
		Selected: page.Selected,
		Items:    conversationHistoryEntries(page.Items, page.RoundID, page.RevisionID, conversationID, s.sessions),
		HasMore:  page.HasMore, NextAfterItemID: page.NextAfterItemID,
	})
}

func (s *Server) handleConversationRoundRevisions(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	conversationID := strings.TrimSpace(r.PathValue("conversation_id"))
	roundID := strings.TrimSpace(r.PathValue("round_id"))
	if agentID == "" || conversationID == "" || roundID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_conversation", "agent_id, conversation_id and round_id are required", nil)
		return
	}
	if agentID != conversationID {
		writeAPIError(w, http.StatusNotFound, "conversation_not_found", "对话不存在", nil)
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "history_unavailable", "历史存储当前不可用", nil)
		return
	}
	page, err := s.store.LoadConversationRoundRevisions(r.Context(), conversationID, roundID)
	if err != nil {
		writeConversationHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationRoundRevisionsResponse{
		AgentID: agentID, ConversationID: page.ConversationID,
		ConversationRevision: page.ConversationRevision, RoundID: page.RoundID,
		SelectedRevisionID: page.SelectedRevisionID, Revisions: page.Revisions,
	})
}

func (s *Server) handleConversationToolSteps(w http.ResponseWriter, r *http.Request) {
	agentID, conversationID, roundID := strings.TrimSpace(r.PathValue("agent_id")), strings.TrimSpace(r.PathValue("conversation_id")), strings.TrimSpace(r.PathValue("round_id"))
	if agentID == "" || conversationID == "" || roundID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_conversation", "agent_id, conversation_id and round_id are required", nil)
		return
	}
	if agentID != conversationID {
		writeAPIError(w, http.StatusNotFound, "conversation_not_found", "对话不存在", nil)
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "history_unavailable", "历史存储当前不可用", nil)
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("source_item_id")) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_history_cursor", "source_item_id is required", nil)
		return
	}
	limit, err := historyPageLimit(r, 50, 50)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_history_limit", err.Error(), nil)
		return
	}
	page, err := s.store.LoadConversationToolSteps(r.Context(), conversationID, roundID, r.URL.Query().Get("revision_id"), r.URL.Query().Get("source_item_id"), r.URL.Query().Get("after_tool_call_id"), limit)
	if err != nil {
		writeConversationHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationToolStepsResponse{
		AgentID: agentID, ConversationID: page.ConversationID, ConversationRevision: page.ConversationRevision,
		RoundID: page.RoundID, RevisionID: page.RevisionID, RevisionStatus: page.RevisionStatus,
		Selected: page.Selected, SourceItemID: page.SourceItemID, TotalCount: page.TotalCount,
		Steps: projectConversationToolSteps(page, false, s.sessions), HasMore: page.HasMore,
		NextAfterToolCallID: page.NextAfterToolCallID,
	})
}

func (s *Server) handleConversationToolStep(w http.ResponseWriter, r *http.Request) {
	agentID, conversationID, roundID := strings.TrimSpace(r.PathValue("agent_id")), strings.TrimSpace(r.PathValue("conversation_id")), strings.TrimSpace(r.PathValue("round_id"))
	toolCallID := strings.TrimSpace(r.PathValue("tool_call_id"))
	if agentID == "" || conversationID == "" || roundID == "" || toolCallID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_conversation", "agent_id, conversation_id, round_id and tool_call_id are required", nil)
		return
	}
	if agentID != conversationID {
		writeAPIError(w, http.StatusNotFound, "conversation_not_found", "对话不存在", nil)
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "history_unavailable", "历史存储当前不可用", nil)
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("source_item_id")) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_history_cursor", "source_item_id is required", nil)
		return
	}
	page, err := s.store.LoadConversationToolStep(r.Context(), conversationID, roundID, r.URL.Query().Get("revision_id"), r.URL.Query().Get("source_item_id"), toolCallID)
	if err != nil {
		writeConversationHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationToolStepsResponse{
		AgentID: agentID, ConversationID: page.ConversationID, ConversationRevision: page.ConversationRevision,
		RoundID: page.RoundID, RevisionID: page.RevisionID, RevisionStatus: page.RevisionStatus,
		Selected: page.Selected, SourceItemID: page.SourceItemID, TotalCount: page.TotalCount,
		Steps: projectConversationToolSteps(page, true, s.sessions),
	})
}

func (s *Server) handleConversationItemContent(w http.ResponseWriter, r *http.Request) {
	agentID, conversationID, roundID := strings.TrimSpace(r.PathValue("agent_id")), strings.TrimSpace(r.PathValue("conversation_id")), strings.TrimSpace(r.PathValue("round_id"))
	itemID := strings.TrimSpace(r.PathValue("item_id"))
	if agentID == "" || conversationID == "" || roundID == "" || itemID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_conversation", "agent_id, conversation_id, round_id and item_id are required", nil)
		return
	}
	if agentID != conversationID {
		writeAPIError(w, http.StatusNotFound, "conversation_not_found", "对话不存在", nil)
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "history_unavailable", "历史存储当前不可用", nil)
		return
	}
	part := strings.TrimSpace(r.URL.Query().Get("part"))
	if part == "" {
		part = "content"
	}
	if part != "content" && part != "reasoning" {
		writeAPIError(w, http.StatusBadRequest, "invalid_history_part", "part must be content or reasoning", nil)
		return
	}
	text, version, selected, err := s.store.LoadConversationItemContent(r.Context(), conversationID, roundID, r.URL.Query().Get("revision_id"), itemID, part)
	if err != nil {
		writeConversationHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": agentID, "conversation_id": conversationID, "round_id": roundID,
		"revision_id": strings.TrimSpace(r.URL.Query().Get("revision_id")), "item_id": itemID,
		"item_version": version, "selected": selected, "part": part, "text": text,
	})
}

func projectConversationToolSteps(page store.ConversationToolStepsPage, includeResult bool, mediaManagers ...*session.Manager) []conversationToolStepView {
	out := make([]conversationToolStepView, 0, len(page.Steps))
	for _, step := range page.Steps {
		callMessage := llm.Message{Role: "assistant", RecordID: step.SourceItemID, ToolCalls: []llm.ToolCall{step.Call}}
		callEntries := session.MessagesToTranscriptEntries([]llm.Message{callMessage})
		if len(callEntries) == 0 {
			continue
		}
		callEntry := callEntries[0]
		decorateToolDetailEntry(callEntry, page, step.SourceItemID, true)
		var resultEntry session.TranscriptEntry
		if step.Result != nil {
			resultEntries := session.MessagesToTranscriptEntries([]llm.Message{callMessage, *step.Result})
			mediaManager := firstMediaManager(mediaManagers)
			if mediaManager != nil {
				session.EnrichTranscriptMediaFromMessages(resultEntries, []llm.Message{callMessage, *step.Result}, mediaManager.SessionMediaRegistry(page.ConversationID))
			}
			for _, entry := range resultEntries {
				if entry["kind"] != "tool_result" {
					continue
				}
				resultEntry = entry
				break
			}
			if resultEntry != nil {
				decorateToolDetailEntry(resultEntry, page, step.Result.RecordID, !includeResult)
				if !includeResult {
					data, _ := resultEntry["data"].(map[string]any)
					if content, ok := data["content"].(string); ok && content != "" {
						data["content_bytes"] = len([]byte(content))
						delete(data, "content")
					}
				}
			}
		}
		if !includeResult {
			data, _ := callEntry["data"].(map[string]any)
			if args, ok := data["arguments"].(map[string]any); ok {
				if purpose, ok := args["call_purpose"]; ok {
					data["arguments"] = map[string]any{"call_purpose": purpose}
				} else {
					delete(data, "arguments")
				}
			}
		}
		out = append(out, conversationToolStepView{ToolCallID: step.ToolCallID, CallEntry: callEntry, ResultEntry: resultEntry})
	}
	if out == nil {
		return []conversationToolStepView{}
	}
	return out
}

func decorateToolDetailEntry(entry session.TranscriptEntry, page store.ConversationToolStepsPage, itemID string, detailsAvailable bool) {
	entry["round_id"], entry["round_revision_id"] = page.RoundID, page.RevisionID
	entry["detail_item_id"] = itemID
	entry["details_available"] = detailsAvailable
}

func (s *Server) handleConversationLegacyItems(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	conversationID := strings.TrimSpace(r.PathValue("conversation_id"))
	if agentID == "" || conversationID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_conversation", "agent_id and conversation_id are required", nil)
		return
	}
	if agentID != conversationID {
		writeAPIError(w, http.StatusNotFound, "conversation_not_found", "对话不存在", nil)
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "history_unavailable", "历史存储当前不可用", nil)
		return
	}
	limit, err := historyPageLimit(r, 50, 200)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_history_limit", err.Error(), nil)
		return
	}
	page, err := s.store.LoadConversationLegacyItems(r.Context(), conversationID, r.URL.Query().Get("after_item_id"), limit)
	if err != nil {
		writeConversationHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationLegacyItemsResponse{
		AgentID: agentID, ConversationID: page.ConversationID,
		ConversationRevision: page.ConversationRevision, HistoryCompleteness: page.HistoryCompleteness, ItemCount: page.ItemCount,
		Items:   conversationHistoryEntries(page.Items, "", "", conversationID, s.sessions),
		HasMore: page.HasMore, NextAfterItemID: page.NextAfterItemID,
	})
}

func conversationHistoryEntries(items []store.ConversationRoundItem, roundID, revisionID, conversationID string, mediaManagers ...*session.Manager) []session.TranscriptEntry {
	messages := make([]llm.Message, 0, len(items))
	versions := make(map[string]uint64, len(items)*2)
	for _, item := range items {
		messages = append(messages, item.Payload)
		versions[item.ItemID] = item.ItemVersion
		if identity := item.Payload.DisplayIdentity; identity != nil {
			if id := strings.TrimSpace(identity.ContentItemID); id != "" {
				versions[id] = item.ItemVersion
			}
			if id := strings.TrimSpace(identity.ReasoningItemID); id != "" {
				versions[id] = item.ItemVersion
			}
		}
		if strings.EqualFold(strings.TrimSpace(item.Payload.Role), "assistant") {
			for _, call := range item.Payload.ToolCalls {
				if id := strings.TrimSpace(call.ID); id != "" {
					versions["tool-call/"+id] = item.ItemVersion
				}
			}
		}
	}
	var registryManager = firstMediaManager(mediaManagers)
	var entries []session.TranscriptEntry
	if registryManager != nil {
		registry := registryManager.SessionMediaRegistry(conversationID)
		entries = session.MessagesToTranscriptEntriesWithMediaAndItemVersions(messages, registry, versions)
		session.EnrichTranscriptMediaFromMessages(entries, messages, registry)
	} else {
		entries = session.MessagesToTranscriptEntriesWithItemVersions(messages, versions)
	}
	orders := make(map[string]store.ConversationRoundItem, len(items)*3)
	for _, item := range items {
		orders[item.ItemID] = item
		if identity := item.Payload.DisplayIdentity; identity != nil {
			orders[identity.ContentItemID] = item
			orders[identity.ReasoningItemID] = item
		}
		for _, call := range item.Payload.ToolCalls {
			orders["tool-call/"+call.ID] = item
		}
	}
	for _, entry := range entries {
		itemID, _ := entry["item_id"].(string)
		item, ok := orders[itemID]
		if !ok {
			continue
		}
		entry["detail_item_id"] = item.ItemID
		entry["round_id"], entry["round_revision_id"] = roundID, revisionID
		entry["conversation_ordinal"] = item.Ordinal
	}
	if roundID == "" {
		return entries
	}
	return session.BoundTranscriptDetails(entries)
}

func firstMediaManager(managers []*session.Manager) *session.Manager {
	if len(managers) == 0 {
		return nil
	}
	return managers[0]
}

func historyPageLimit(r *http.Request, defaultLimit, maximum int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maximum {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximum)
	}
	return limit, nil
}

func writeConversationHistoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrConversationHistoryNotFound), errors.Is(err, store.ErrConversationRoundNotFound):
		writeAPIError(w, http.StatusNotFound, "conversation_history_not_found", "对话历史或轮次不存在", nil)
	case errors.Is(err, store.ErrConversationHistoryCursor):
		writeAPIError(w, http.StatusConflict, "history_cursor_stale", "历史位置已失效，请重新加载历史", nil)
	case errors.Is(err, store.ErrConversationRoundActive):
		writeAPIError(w, http.StatusConflict, "history_round_active", "当前轮次仍在执行中，请从聊天页面查看", nil)
	default:
		writeAPIError(w, http.StatusInternalServerError, "history_read_failed", "读取对话历史失败", nil)
	}
}
