package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type turnTimelineEvent struct {
	ID              int64          `json:"id"`
	AgentID         string         `json:"agent_id,omitempty"`
	SessionID       string         `json:"session_id"`
	TurnID          string         `json:"turn_id"`
	StepID          string         `json:"step_id,omitempty"`
	ToolBatchID     string         `json:"tool_batch_id,omitempty"`
	ToolCallID      string         `json:"tool_call_id,omitempty"`
	ToolExecutionID string         `json:"tool_execution_id,omitempty"`
	InteractionID   string         `json:"interaction_id,omitempty"`
	SessionSeq      uint64         `json:"session_seq"`
	TurnSeq         uint64         `json:"turn_seq"`
	EventType       turn.EventType `json:"event_type"`
	EventVersion    int            `json:"event_version"`
	Source          string         `json:"source,omitempty"`
	CommandID       string         `json:"command_id,omitempty"`
	Payload         map[string]any `json:"payload,omitempty"`
	PayloadRef      string         `json:"payload_ref,omitempty"`
	CreatedAt       string         `json:"created_at"`
}

type turnTimelineResponse struct {
	AgentID  string              `json:"agent_id"`
	AfterSeq uint64              `json:"after_seq"`
	NextSeq  uint64              `json:"next_seq"`
	Events   []turnTimelineEvent `json:"events"`
}

func (s *Server) handleAgentTimeline(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	if agentID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": agentID})
		return
	}
	if s.agents != nil {
		record, err := s.agents.Get(r.Context(), agentID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
			return
		}
		if record == nil || record.Archived {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": agentID})
			return
		}
	}
	afterSeq, err := parseTimelineSeq(r.URL.Query().Get("after_seq"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_after_seq", err.Error(), nil)
		return
	}
	limit := 1000
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			writeAPIError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 1000", nil)
			return
		}
	}
	events, err := s.store.ListTurnEvents(r.Context(), agentID, afterSeq, limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	response := turnTimelineResponse{AgentID: agentID, AfterSeq: afterSeq, Events: make([]turnTimelineEvent, 0, len(events))}
	for _, event := range events {
		var payload map[string]any
		if len(event.Payload) > 0 && string(event.Payload) != "null" {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		response.Events = append(response.Events, turnTimelineEvent{
			ID: event.ID, AgentID: event.AgentID, SessionID: event.SessionID,
			TurnID: event.TurnID, StepID: event.StepID, ToolBatchID: event.ToolBatchID,
			ToolCallID: event.ToolCallID, ToolExecutionID: event.ToolExecutionID,
			InteractionID: event.InteractionID, SessionSeq: event.SessionSeq,
			TurnSeq: event.TurnSeq, EventType: event.EventType, EventVersion: event.EventVersion,
			Source: event.Source, CommandID: event.CommandID, Payload: payload,
			PayloadRef: event.PayloadRef, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
		if event.SessionSeq > response.NextSeq {
			response.NextSeq = event.SessionSeq
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func parseTimelineSeq(raw string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	seq, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

type reconcileToolExecutionRequest struct {
	Status  string `json:"status"`
	Content string `json:"content,omitempty"`
}

type reconcileToolExecutionResponse struct {
	Accepted        bool   `json:"accepted"`
	AgentID         string `json:"agent_id"`
	TurnID          string `json:"turn_id"`
	StepID          string `json:"step_id"`
	ToolExecutionID string `json:"tool_execution_id"`
	Status          string `json:"status"`
}

func (s *Server) handleAgentReconcileToolExecution(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	turnID := strings.TrimSpace(r.PathValue("turn_id"))
	stepID := strings.TrimSpace(r.PathValue("step_id"))
	executionID := strings.TrimSpace(r.PathValue("execution_id"))
	if agentID == "" || turnID == "" || stepID == "" || executionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_reconciliation", "agent, turn, step and execution identifiers are required", nil)
		return
	}
	var request reconcileToolExecutionRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	status := turn.ToolExecutionStatus(strings.TrimSpace(request.Status))
	if !status.Terminal() || status == turn.ToolExecutionStatusUnknown {
		writeAPIError(w, http.StatusBadRequest, "invalid_reconciliation_status", "status must be a known terminal tool execution status", nil)
		return
	}
	if s.sessions == nil {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": agentID})
		return
	}
	// A reconciliation request is commonly the first request after a Node
	// restart. Hydrate the agent from agents.db before looking up its runtime;
	// otherwise the recovery fence would be impossible to clear until a client
	// made an unrelated ensure call first.
	if s.agents != nil {
		if err := s.ensureAgentRuntime(r.Context(), agentID); err != nil {
			if err.Error() == "agent_not_found" {
				writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": agentID})
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", err.Error(), map[string]any{"agent_id": agentID})
			return
		}
	}
	err := s.sessions.ReconcileToolExecution(r.Context(), agentID, turnID, stepID, executionID, status, request.Content)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": agentID})
			return
		}
		writeAPIError(w, http.StatusConflict, "reconciliation_rejected", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, reconcileToolExecutionResponse{
		Accepted: true, AgentID: agentID, TurnID: turnID, StepID: stepID,
		ToolExecutionID: executionID, Status: string(status),
	})
}
