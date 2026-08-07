package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/node/internal/workgroup"
)

func (s *Server) registerWorkgroupRoutes() {
	s.mux.HandleFunc("GET /v1/workgroups", s.handleListWorkgroups)
	s.mux.HandleFunc("POST /v1/workgroups", s.handleCreateWorkgroup)
	s.mux.HandleFunc("GET /v1/workgroups/meta/member-tools", s.handleGetMemberToolCatalog)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}", s.handleGetWorkgroup)
	s.mux.HandleFunc("PATCH /v1/workgroups/{workgroupId}", s.handlePatchWorkgroup)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/publish", s.handlePublishWorkgroup)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/llm-configs", s.handleListWorkgroupLLMConfigs)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/acl", s.handleGetWorkgroupACL)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/collaborators", s.handleAddWorkgroupCollaborator)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/members", s.handleListWorkgroupMembers)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/members", s.handleCreateWorkgroupMember)
	s.mux.HandleFunc("PATCH /v1/workgroups/{workgroupId}/members/{memberId}", s.handlePatchWorkgroupMember)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/members/{memberId}/spec", s.handleGetWorkgroupMemberSpec)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/members/{memberId}/archive", s.handleArchiveWorkgroupMember)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/hitl", s.handleListWorkgroupHITL)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/hitl", s.handleCreateWorkgroupHITL)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/hitl/{hitlId}/resolve", s.handleResolveWorkgroupHITL)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/subscribe", s.handleSubscribeWorkgroup)
	s.mux.HandleFunc("DELETE /v1/workgroups/{workgroupId}/subscribe", s.handleUnsubscribeWorkgroup)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/timeline", s.handleWorkgroupTimeline)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/messages", s.handlePostWorkgroupMessage)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/messages/stream", s.handlePostWorkgroupMessageStream)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/human-queue", s.handleWorkgroupHumanQueue)
	s.mux.HandleFunc("PATCH /v1/workgroups/{workgroupId}/human-queue/{queueId}", s.handlePatchWorkgroupHumanQueue)
	s.mux.HandleFunc("DELETE /v1/workgroups/{workgroupId}/human-queue/{queueId}", s.handleDeleteWorkgroupHumanQueue)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/turn/cancel", s.handleCancelWorkgroupTurn)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/runs", s.handleListWorkgroupRuns)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/runs/{runId}/history", s.handleGetWorkgroupRunHistory)
}

func (s *Server) workgroupProxyReady(w http.ResponseWriter) bool {
	if s == nil || s.control == nil || !s.cfg.Manage.Enabled {
		writeAPIError(w, http.StatusServiceUnavailable, "manage_disabled", "manage is not enabled", nil)
		return false
	}
	if !s.cfg.ManageWorkgroupEnabled() {
		writeAPIError(w, http.StatusServiceUnavailable, "workgroup_disabled", "manage.workgroup is disabled", nil)
		return false
	}
	return true
}

func (s *Server) handleListWorkgroups(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	mode := manage.WorkgroupListSubscribed
	switch strings.TrimSpace(r.URL.Query().Get("scope")) {
	case "all":
		mode = manage.WorkgroupListAll
	case "acl", "available":
		mode = manage.WorkgroupListACL
	case "subscribed", "":
		if r.URL.Query().Get("subscribed") == "0" {
			mode = manage.WorkgroupListAll
		} else {
			mode = manage.WorkgroupListSubscribed
		}
	}
	// 兼容旧 query
	if r.URL.Query().Get("subscribed") == "1" || r.URL.Query().Get("subscribed_by") != "" {
		mode = manage.WorkgroupListSubscribed
	}
	items, err := s.control.ListWorkgroups(r.Context(), mode)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workgroups": items, "scope": string(mode)})
}

func (s *Server) handleCreateWorkgroup(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	var body struct {
		DisplayName        string `json:"display_name"`
		LLMProfileID       string `json:"llm_profile_id"`
		LLMProfileRevision string `json:"llm_profile_revision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	out, err := s.control.CreateWorkgroup(r.Context(), manage.CreateWorkgroupInput{
		DisplayName:        body.DisplayName,
		LLMProfileID:       body.LLMProfileID,
		LLMProfileRevision: body.LLMProfileRevision,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetWorkgroup(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	out, err := s.control.GetWorkgroup(r.Context(), wid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePatchWorkgroup(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	out, err := s.control.PatchWorkgroup(r.Context(), wid, body)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePublishWorkgroup(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	out, err := s.control.PublishWorkgroup(r.Context(), wid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListWorkgroupLLMConfigs(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	items, err := s.control.ListWorkgroupLLMConfigs(r.Context(), wid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configs": items})
}

func (s *Server) handleGetMemberToolCatalog(w http.ResponseWriter, r *http.Request) {
	// 目录来自仓库嵌入 JSON；Node 单机不连 Manage 也可读。不经 Manage HTTP。
	_ = r
	writeJSON(w, http.StatusOK, workgroup.MemberToolCatalogAPI())
}

func (s *Server) handleGetWorkgroupACL(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	acl, err := s.control.GetWorkgroupACL(r.Context(), wid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, acl)
}

func (s *Server) handleAddWorkgroupCollaborator(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	var body struct {
		NodeID string `json:"node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	if strings.TrimSpace(body.NodeID) == "" {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "node_id required", nil)
		return
	}
	acl, err := s.control.AddWorkgroupCollaborator(r.Context(), wid, body.NodeID)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, acl)
}

func (s *Server) handleListWorkgroupMembers(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	items, err := s.control.ListWorkgroupMembers(r.Context(), wid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": items, "node_id": s.cfg.NodeID})
}

func (s *Server) handleCreateWorkgroupMember(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	if strings.TrimSpace(strAny(body["home_node_id"])) == "" {
		body["home_node_id"] = s.cfg.NodeID
	}
	if tools, ok := body["allow_tool_names"].([]any); !ok || len(tools) == 0 {
		body["allow_tool_names"] = workgroup.WorkspaceDefaultAllowToolNames()
	}
	home := strings.TrimSpace(strAny(body["home_node_id"]))
	if home != "" {
		if _, err := s.control.AddWorkgroupCollaborator(r.Context(), wid, home); err != nil {
			writeAPIError(w, http.StatusBadGateway, "manage_error", "ensure home in ACL: "+err.Error(), nil)
			return
		}
	}
	out, err := s.control.CreateWorkgroupMember(r.Context(), wid, body)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePatchWorkgroupMember(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	mid := strings.TrimSpace(r.PathValue("memberId"))
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	out, err := s.control.PatchWorkgroupMember(r.Context(), wid, mid, body)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetWorkgroupMemberSpec(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	mid := strings.TrimSpace(r.PathValue("memberId"))
	out, err := s.control.GetWorkgroupMemberSpec(r.Context(), wid, mid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleArchiveWorkgroupMember(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	mid := strings.TrimSpace(r.PathValue("memberId"))
	out, err := s.control.ArchiveWorkgroupMember(r.Context(), wid, mid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListWorkgroupHITL(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	pendingOnly := r.URL.Query().Get("pending_only") != "false"
	items, err := s.control.ListWorkgroupHITL(r.Context(), wid, pendingOnly)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hitl": items})
}

func (s *Server) handleCreateWorkgroupHITL(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "prompt required", nil)
		return
	}
	out, err := s.control.CreateWorkgroupHITL(r.Context(), wid, body.Prompt)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleResolveWorkgroupHITL(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	hid := strings.TrimSpace(r.PathValue("hitlId"))
	var body struct {
		Resolution map[string]any `json:"resolution"`
		Answer     string         `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	res := body.Resolution
	if res == nil {
		res = map[string]any{}
	}
	if strings.TrimSpace(body.Answer) != "" {
		res["answer"] = body.Answer
	}
	out, err := s.control.ResolveWorkgroupHITL(r.Context(), wid, hid, res)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func strAny(v any) string {
	s, _ := v.(string)
	return s
}

func (s *Server) handleSubscribeWorkgroup(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	if err := s.control.SubscribeWorkgroup(r.Context(), wid); err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "workgroup_id": wid, "node_id": s.cfg.NodeID})
}

func (s *Server) handleUnsubscribeWorkgroup(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	if err := s.control.UnsubscribeWorkgroup(r.Context(), wid); err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleWorkgroupTimeline(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	events, err := s.control.GetWorkgroupTimeline(r.Context(), wid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handlePostWorkgroupMessage(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	var body struct {
		Text            string `json:"text"`
		ClientMessageID string `json:"client_message_id"`
		DirectMemberID  string `json:"direct_member_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "text required", nil)
		return
	}
	ev, err := s.control.PostWorkgroupMessage(r.Context(), wid, body.Text, body.ClientMessageID, body.DirectMemberID)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (s *Server) handlePostWorkgroupMessageStream(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	var body struct {
		Text            string `json:"text"`
		ClientMessageID string `json:"client_message_id"`
		DirectMemberID  string `json:"direct_member_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "text required", nil)
		return
	}

	upstream, err := s.control.OpenWorkgroupMessageStream(
		r.Context(),
		wid,
		body.Text,
		body.ClientMessageID,
		body.DirectMemberID,
	)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	defer upstream.Body.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "stream_unsupported", "streaming unsupported", nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	buf := make([]byte, 4096)
	for {
		n, readErr := upstream.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			if readErr != io.EOF {
				// 客户端断开或上游中断：静默结束
			}
			return
		}
	}
}

func (s *Server) handleCancelWorkgroupTurn(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	out, err := s.control.CancelWorkgroupTurn(r.Context(), wid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListWorkgroupRuns(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	actorID := strings.TrimSpace(r.URL.Query().Get("actor_id"))
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	out, err := s.control.ListWorkgroupRuns(r.Context(), wid, actorID, limit)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetWorkgroupRunHistory(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	runID := strings.TrimSpace(r.PathValue("runId"))
	out, err := s.control.GetWorkgroupRunHistory(r.Context(), wid, runID)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleWorkgroupHumanQueue(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	out, err := s.control.ListWorkgroupHumanQueue(r.Context(), wid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePatchWorkgroupHumanQueue(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	qid := strings.TrimSpace(r.PathValue("queueId"))
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	out, err := s.control.PatchWorkgroupHumanQueueItem(r.Context(), wid, qid, body.Text)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteWorkgroupHumanQueue(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	qid := strings.TrimSpace(r.PathValue("queueId"))
	out, err := s.control.CancelWorkgroupHumanQueueItem(r.Context(), wid, qid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
