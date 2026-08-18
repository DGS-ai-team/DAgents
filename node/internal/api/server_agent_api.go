package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type healthResponse struct {
	Status  string `json:"status"`
	NodeID  string `json:"node_id"`
	Version string `json:"version"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		NodeID:  s.cfg.NodeID,
		Version: version.Version,
	})
}

type agentInfoResponse struct {
	NodeID            string              `json:"node_id"`
	Name              string              `json:"name,omitempty"`
	Capabilities      []string            `json:"capabilities"`
	MultimodalEnabled bool                `json:"multimodal_enabled"`
	ManageEnabled     bool                `json:"manage_enabled"`
	ManageURL         string              `json:"manage_url,omitempty"`
	ManageRegistered  bool                `json:"manage_registered"`
	LLM               llm.LLMSettingsView `json:"llm"`
	Compression       compressionInfo     `json:"compression"`
}

type compressionInfo struct {
	SilentTriggerTokens   int `json:"silent_trigger_tokens"`
	BlockingTriggerTokens int `json:"blocking_trigger_tokens"`
}

func (s *Server) handleAgentInfo(w http.ResponseWriter, _ *http.Request) {
	registered := false
	if s.registrar != nil {
		registered = s.registrar.Registered()
	}
	llmView := llm.LLMSettingsView{}
	if s.llmRuntime != nil {
		llmView = s.llmRuntime.Snapshot()
	}
	comp := compressionInfo{}
	if s.cfg != nil {
		comp.SilentTriggerTokens = s.cfg.Compression.SilentTriggerTokens
		comp.BlockingTriggerTokens = s.cfg.Compression.BlockingTriggerTokens
	}
	writeJSON(w, http.StatusOK, agentInfoResponse{
		NodeID:            s.cfg.NodeID,
		Name:              strings.TrimSpace(s.cfg.Agent.Name),
		Capabilities:      s.cfg.Capabilities(),
		MultimodalEnabled: s.cfg.MultimodalEnabled(),
		ManageEnabled:     s.cfg != nil && s.cfg.Manage.Enabled,
		ManageURL:         strings.TrimSpace(s.cfg.Manage.URL),
		ManageRegistered:  registered,
		LLM:               llmView,
		Compression:       comp,
	})
}

func (s *Server) handleAgentUpdate(w http.ResponseWriter, _ *http.Request) {
	if manage.UpdateDelegatedToShell() {
		channel := "stable"
		if s.cfg != nil {
			channel = strings.TrimSpace(s.cfg.Manage.Update.Channel)
		}
		writeJSON(w, http.StatusOK, manage.ShellDelegateUpdateStatus(channel))
		return
	}
	if s.updateChecker == nil {
		writeJSON(w, http.StatusOK, manage.UpdateStatus{
			CurrentVersion:  version.Version,
			LatestVersion:   version.Version,
			ManageReachable: false,
			Platform:        manage.ReleasePlatform(),
			Channel:         "stable",
			ApplyCommand:    "dagents update",
			Message:         "Manage 未启用，无法检查更新",
		})
		return
	}
	writeJSON(w, http.StatusOK, s.updateChecker.Snapshot())
}

func (s *Server) handleAgentUpgradeReadiness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.sessions.UpgradeReadiness())
}

type clearContextResponse struct {
	AgentID       string `json:"agent_id"`
	Cleared       bool   `json:"cleared"`
	CancelledTurn bool   `json:"cancelled_turn"`
}

func (s *Server) handleAgentClearContextImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	cancelled, err := s.sessions.ClearContext(sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, clearContextResponse{
		AgentID:       sessionID,
		Cleared:       true,
		CancelledTurn: cancelled,
	})
}

type contextMessagePreview struct {
	Role                string `json:"role"`
	Content             string `json:"content,omitempty"`
	ToolCallID          string `json:"tool_call_id,omitempty"`
	ToolCallsCount      int    `json:"tool_calls_count,omitempty"`
	HasReasoningContent bool   `json:"has_reasoning_content,omitempty"`
}

type sessionContextResponse struct {
	AgentID                             string                               `json:"agent_id"`
	MessagesCount                       int                                  `json:"messages_count"`
	PendingToolCallsCount               int                                  `json:"pending_tool_calls_count"`
	MessagesTotalTokens                 int                                  `json:"messages_total_tokens"`
	ToolLoopCount                       int                                  `json:"tool_loop_count"`
	QueuePending                        int                                  `json:"queue_pending"`
	HasActiveTurn                       bool                                 `json:"has_active_turn"`
	TurnState                           string                               `json:"turn_state,omitempty"`
	RunTurnPhase                        string                               `json:"run_turn_phase"`
	SystemPrompt                        string                               `json:"system_prompt,omitempty"`
	SystemPromptEstimatedTokens         int                                  `json:"system_prompt_estimated_tokens"`
	SkillsCatalogEstimatedTokens        int                                  `json:"skills_catalog_estimated_tokens"`
	SkillsCatalogMaxBodyEstimatedTokens int                                  `json:"skills_catalog_max_body_estimated_tokens"`
	SkillsCatalogBloatThreshold         int                                  `json:"skills_catalog_bloat_threshold"`
	LoadedSkills                        []skills.LoadedSkill                 `json:"loaded_skills"`
	RecentMessages                      []contextMessagePreview              `json:"recent_messages"`
	Messages                            *[]contextMessagePreview             `json:"messages,omitempty"`
	LastCompression                     *compression.LastCompressionSnapshot `json:"last_compression,omitempty"`
}

func buildContextMessagePreviews(messages []llm.Message, maxRunes int) []contextMessagePreview {
	out := make([]contextMessagePreview, 0, len(messages))
	for _, m := range messages {
		content := truncateContextPreview(llm.MessageTextSummary(m), maxRunes)
		out = append(out, contextMessagePreview{
			Role:                m.Role,
			Content:             content,
			ToolCallID:          m.ToolCallID,
			ToolCallsCount:      len(m.ToolCalls),
			HasReasoningContent: strings.TrimSpace(m.ReasoningContent) != "",
		})
	}
	return out
}

func queryBoolParam(r *http.Request, key string) bool {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func (s *Server) handleAgentContextImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	view, err := s.sessions.GetContextView(sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	const previewLimit = 10
	const contextMessagePreviewRunes = 8000
	start := 0
	if len(view.Messages) > previewLimit {
		start = len(view.Messages) - previewLimit
	}
	recent := buildContextMessagePreviews(view.Messages[start:], contextMessagePreviewRunes)
	resp := sessionContextResponse{
		AgentID:                             view.SessionID,
		MessagesCount:                       view.MessagesCount,
		PendingToolCallsCount:               view.PendingToolCallsCount,
		MessagesTotalTokens:                 view.MessagesTotalTokens,
		ToolLoopCount:                       view.ToolLoopCount,
		QueuePending:                        view.QueuePending,
		HasActiveTurn:                       view.HasActiveTurn,
		SystemPrompt:                        view.SystemPrompt,
		SystemPromptEstimatedTokens:         view.SystemPromptEstimatedTokens,
		SkillsCatalogEstimatedTokens:        view.SkillsCatalogEstimatedTokens,
		SkillsCatalogMaxBodyEstimatedTokens: view.SkillsCatalogMaxBodyEstimatedTokens,
		SkillsCatalogBloatThreshold:         view.SkillsCatalogBloatThreshold,
		LoadedSkills:                        view.LoadedSkills,
		RecentMessages:                      recent,
		LastCompression:                     view.LastCompression,
		RunTurnPhase:                        turn.RunTurnPhase(view.TurnState),
	}
	if queryBoolParam(r, "full_messages") {
		msgs := buildContextMessagePreviews(view.Messages, contextMessagePreviewRunes)
		resp.Messages = &msgs
	}
	if view.TurnState != "" {
		resp.TurnState = string(view.TurnState)
	}
	if resp.LoadedSkills == nil {
		resp.LoadedSkills = []skills.LoadedSkill{}
	}
	writeJSON(w, http.StatusOK, resp)
}

type sessionHydrateResponse struct {
	AgentID       string                    `json:"agent_id"`
	RunTurnPhase  string                    `json:"run_turn_phase"`
	HasActiveTurn bool                      `json:"has_active_turn"`
	QueuePending  int                       `json:"queue_pending"`
	Transcript    []session.TranscriptEntry `json:"transcript"`
	PendingHITL   map[string]any            `json:"pending_hitl"`
	SSESeqHint    int                       `json:"sse_seq_hint"`
	NotifySeq     int                       `json:"notify_seq"`
	AckSeq        int                       `json:"ack_seq"`
	HasUnread     bool                      `json:"has_unread"`
	ToolJobs      map[string]int            `json:"tool_jobs,omitempty"`
}

func (s *Server) handleAgentHydrateImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	view, err := s.sessions.GetHydrateView(sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	transcript := view.Transcript
	if transcript == nil {
		transcript = []session.TranscriptEntry{}
	}
	toolJobs := map[string]int{"running": 0, "background": 0}
	if reg := s.sessions.SessionTools(sessionID); reg != nil {
		c := reg.SessionToolJobCounts(sessionID)
		toolJobs["running"] = c.Running
		toolJobs["background"] = c.Background
	}
	writeJSON(w, http.StatusOK, sessionHydrateResponse{
		AgentID:       view.SessionID,
		RunTurnPhase:  view.RunTurnPhase,
		HasActiveTurn: view.HasActiveTurn,
		QueuePending:  view.QueuePending,
		Transcript:    transcript,
		PendingHITL:   view.PendingHITL,
		SSESeqHint:    s.stream.CurrentSeq(),
		NotifySeq:     view.NotifySeq,
		AckSeq:        view.AckSeq,
		HasUnread:     view.HasUnread,
		ToolJobs:      toolJobs,
	})
}

type sessionAckRequest struct {
	SSESeq int `json:"sse_seq"`
}

type sessionAckResponse struct {
	AgentID   string `json:"agent_id"`
	NotifySeq int    `json:"notify_seq"`
	AckSeq    int    `json:"ack_seq"`
	HasUnread bool   `json:"has_unread"`
}

func (s *Server) handleAgentAckImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	var req sessionAckRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if req.SSESeq <= 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "sse_seq must be positive", nil)
		return
	}
	state, err := s.sessions.AckSession(r.Context(), sessionID, req.SSESeq)
	if err != nil {
		switch err.Error() {
		case "agent_not_found":
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		case "agent_id is required", "sse_seq must be positive":
			writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		default:
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, sessionAckResponse{
		AgentID:   sessionID,
		NotifySeq: state.NotifySeq,
		AckSeq:    state.AckSeq,
		HasUnread: state.HasUnread,
	})
}

func (s *Server) handleAgentCompressImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	result, err := s.sessions.CompressContext(r.Context(), sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	if result.Status == "busy" {
		writeAPIError(w, http.StatusConflict, "turn_busy", "当前 turn 进行中，请稍后再试", map[string]any{
			"agent_id": sessionID,
			"status":   result.Status,
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAgentListSkillsImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	loaded, available, err := s.sessions.ListSessionSkills(sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":         sessionID,
		"loaded_skills":    loaded,
		"available_skills": available,
	})
}

func (s *Server) handleNodeSkillsCatalog(w http.ResponseWriter, _ *http.Request) {
	if s.cfg == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":          true,
			"available_skills": []skills.LoadedSkill{},
		})
		return
	}
	catalog := skills.NewCatalog(s.cfg.SkillsRoot(), true, s.cfg.Skills.MaxInPrompt)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":          true,
		"skills_root":      catalog.Root(),
		"available_skills": catalog.ListMetadata(),
	})
}

type skillNameRequest struct {
	SkillName string `json:"skill_name"`
}

func (s *Server) handleAgentLoadSkillImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	var req skillNameRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	loaded, err := s.sessions.LoadSessionSkill(sessionID, req.SkillName)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":      sessionID,
		"loaded_skills": loaded,
	})
}

func (s *Server) handleAgentUnloadSkillImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	var req skillNameRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	loaded, err := s.sessions.UnloadSessionSkill(sessionID, req.SkillName)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":      sessionID,
		"loaded_skills": loaded,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func toolsBashCompressFromConfig(toolsCfg config.ToolsConfig) tools.BashCompressConfig {
	out := tools.DefaultBashCompressConfig()
	if toolsCfg.BashCompress.Enabled != nil {
		out.Enabled = *toolsCfg.BashCompress.Enabled
	}
	if toolsCfg.BashCompress.MaxOutputChars > 0 {
		out.MaxOutputChars = toolsCfg.BashCompress.MaxOutputChars
	}
	if toolsCfg.BashCompress.MaxOutputCharsStderr > 0 {
		out.MaxOutputCharsStderr = toolsCfg.BashCompress.MaxOutputCharsStderr
	}
	return out
}
