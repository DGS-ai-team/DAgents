package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/manage"
)

func (s *Server) registerWorkgroupRoutes() {
	s.mux.HandleFunc("GET /v1/workgroups", s.handleListWorkgroups)
	s.mux.HandleFunc("GET /v1/workgroups/desktop/list", s.handleDesktopWorkgroupList)
	s.mux.HandleFunc("POST /v1/workgroups", s.handleCreateWorkgroup)
	s.mux.HandleFunc("GET /v1/workgroups/meta/agents", s.handleListRegisteredAgents)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}", s.handleGetWorkgroup)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/desktop-snapshot", s.handleDesktopWorkgroupSnapshot)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/desktop-progress", s.handleDesktopWorkgroupProgress)
	s.mux.HandleFunc("PATCH /v1/workgroups/{workgroupId}", s.handlePatchWorkgroup)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/publish", s.handlePublishWorkgroup)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/llm-configs", s.handleListWorkgroupLLMConfigs)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/acl", s.handleGetWorkgroupACL)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/collaborators", s.handleAddWorkgroupCollaborator)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/members", s.handleListWorkgroupMembers)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/members", s.handleCreateWorkgroupMember)
	s.mux.HandleFunc("PATCH /v1/workgroups/{workgroupId}/members/{memberId}", s.handlePatchWorkgroupMember)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/members/{memberId}/archive", s.handleArchiveWorkgroupMember)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/hitl", s.handleListWorkgroupHITL)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/hitl", s.handleCreateWorkgroupHITL)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/hitl/{hitlId}/resolve", s.handleResolveWorkgroupHITL)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/subscribe", s.handleSubscribeWorkgroup)
	s.mux.HandleFunc("DELETE /v1/workgroups/{workgroupId}/subscribe", s.handleUnsubscribeWorkgroup)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/events", s.handleWorkgroupEvents)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/timeline", s.handleWorkgroupTimeline)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/messages", s.handlePostWorkgroupMessage)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/messages/stream", s.handlePostWorkgroupMessageStream)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/human-queue", s.handleWorkgroupHumanQueue)
	s.mux.HandleFunc("PATCH /v1/workgroups/{workgroupId}/human-queue/{queueId}", s.handlePatchWorkgroupHumanQueue)
	s.mux.HandleFunc("DELETE /v1/workgroups/{workgroupId}/human-queue/{queueId}", s.handleDeleteWorkgroupHumanQueue)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/human-queue/{queueId}/send-now", s.handleSendWorkgroupHumanQueueNow)
	s.mux.HandleFunc("POST /v1/workgroups/{workgroupId}/turn/cancel", s.handleCancelWorkgroupTurn)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/runs", s.handleListWorkgroupRuns)
	s.mux.HandleFunc("GET /v1/workgroups/{workgroupId}/runs/{runId}/history", s.handleGetWorkgroupRunHistory)
}

type desktopWorkgroupListItem struct {
	manage.WorkgroupListItem
	Subscribed bool `json:"subscribed"`
}

func (s *Server) handleDesktopWorkgroupList(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	type listResult struct {
		items []manage.WorkgroupListItem
		err   error
	}
	allResult := make(chan listResult, 1)
	subscribedResult := make(chan listResult, 1)
	go func() {
		items, err := s.control.ListWorkgroups(r.Context(), manage.WorkgroupListAll)
		allResult <- listResult{items: items, err: err}
	}()
	go func() {
		items, err := s.control.ListWorkgroups(r.Context(), manage.WorkgroupListSubscribed)
		subscribedResult <- listResult{items: items, err: err}
	}()
	allResponse, subscribedResponse := <-allResult, <-subscribedResult
	if allResponse.err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", allResponse.err.Error(), nil)
		return
	}
	if subscribedResponse.err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", subscribedResponse.err.Error(), nil)
		return
	}
	all, subscribed := allResponse.items, subscribedResponse.items
	subscribedIDs := make(map[string]struct{}, len(subscribed))
	for _, item := range subscribed {
		subscribedIDs[item.WorkgroupID] = struct{}{}
	}
	items := make([]desktopWorkgroupListItem, 0, len(all))
	for _, item := range all {
		_, isSubscribed := subscribedIDs[item.WorkgroupID]
		items = append(items, desktopWorkgroupListItem{WorkgroupListItem: item, Subscribed: isSubscribed})
	}
	writeJSON(w, http.StatusOK, map[string]any{"workgroups": items, "scope": "desktop"})
}

func (s *Server) handleDesktopWorkgroupSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	if wid == "" {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "workgroup_id required", nil)
		return
	}
	group, err := s.control.GetWorkgroup(r.Context(), wid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	groupView := group
	if nested, ok := group["workgroup"].(map[string]any); ok {
		groupView = nested
	}
	status := strings.TrimSpace(strAny(groupView["status"]))
	type result struct {
		key   string
		value any
		err   error
	}
	tasks := []struct {
		key string
		fn  func() (any, error)
	}{
		{"members", func() (any, error) { return s.control.ListWorkgroupMembers(r.Context(), wid) }},
		{"events", func() (any, error) { return s.control.GetWorkgroupTimeline(r.Context(), wid) }},
		{"hitl", func() (any, error) { return s.control.ListWorkgroupHITL(r.Context(), wid, true) }},
	}
	if r.URL.Query().Get("include_agents") == "true" {
		tasks = append(tasks, struct {
			key string
			fn  func() (any, error)
		}{"agents", func() (any, error) { return s.control.ListRegisteredAgents(r.Context()) }})
	}
	if status == "active" {
		tasks = append(tasks, struct {
			key string
			fn  func() (any, error)
		}{"human_queue", func() (any, error) { return s.control.ListWorkgroupHumanQueue(r.Context(), wid) }})
	}
	if status == "configuring" {
		tasks = append(tasks, struct {
			key string
			fn  func() (any, error)
		}{"llm_configs", func() (any, error) { return s.control.ListWorkgroupLLMConfigs(r.Context(), wid) }})
	}
	results := make(chan result, len(tasks))
	for _, task := range tasks {
		task := task
		go func() {
			value, callErr := task.fn()
			results <- result{key: task.key, value: value, err: callErr}
		}()
	}
	snapshot := map[string]any{"workgroup": group, "node_id": s.cfg.NodeID}
	for range tasks {
		item := <-results
		if item.err != nil {
			writeAPIError(w, http.StatusBadGateway, "manage_error", item.err.Error(), map[string]any{"section": item.key})
			return
		}
		snapshot[item.key] = item.value
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleDesktopWorkgroupProgress(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	if wid == "" {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "workgroup_id required", nil)
		return
	}
	type result struct {
		key   string
		value any
		err   error
	}
	results := make(chan result, 2)
	go func() {
		value, err := s.control.GetWorkgroupTimeline(r.Context(), wid)
		results <- result{key: "events", value: value, err: err}
	}()
	go func() {
		value, err := s.control.ListWorkgroupHumanQueue(r.Context(), wid)
		results <- result{key: "human_queue", value: value, err: err}
	}()
	payload := make(map[string]any, 2)
	for range 2 {
		item := <-results
		if item.err != nil {
			writeAPIError(w, http.StatusBadGateway, "manage_error", item.err.Error(), map[string]any{"section": item.key})
			return
		}
		payload[item.key] = item.value
	}
	writeJSON(w, http.StatusOK, payload)
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
	case "acl":
		mode = manage.WorkgroupListACL
	case "subscribed", "":
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

func (s *Server) handleListRegisteredAgents(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	items, err := s.control.ListRegisteredAgents(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": items})
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
	limit := 0
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsed < 0 {
			writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "limit must be a non-negative integer", nil)
			return
		}
		limit = parsed
	}
	events, err := s.control.GetWorkgroupTimeline(r.Context(), wid, limit)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleWorkgroupEvents(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok || s.workgroupStream == nil {
		writeAPIError(w, http.StatusInternalServerError, "stream_unsupported", "streaming unsupported", nil)
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	if wid == "" {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "workgroup_id required", nil)
		return
	}
	subscribed, err := s.control.ListWorkgroups(r.Context(), manage.WorkgroupListSubscribed)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	allowed := false
	for _, item := range subscribed {
		if strings.TrimSpace(item.WorkgroupID) == wid {
			allowed = true
			break
		}
	}
	if !allowed {
		writeAPIError(w, http.StatusForbidden, "not_subscribed", "workgroup is not subscribed by this node", nil)
		return
	}
	lastSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))
	if afterRaw := strings.TrimSpace(r.URL.Query().Get("after_seq")); afterRaw != "" {
		lastSeq = parseLastEventID(afterRaw)
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	events := s.workgroupStream.SubscribeAgent(lastSeq, wid)
	defer s.workgroupStream.Unsubscribe(events)
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
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

func (s *Server) handleSendWorkgroupHumanQueueNow(w http.ResponseWriter, r *http.Request) {
	if !s.workgroupProxyReady(w) {
		return
	}
	wid := strings.TrimSpace(r.PathValue("workgroupId"))
	qid := strings.TrimSpace(r.PathValue("queueId"))
	out, err := s.control.SendWorkgroupHumanQueueItemNow(r.Context(), wid, qid)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
