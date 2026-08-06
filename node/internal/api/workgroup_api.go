package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/manage"
)

func (s *Server) registerWorkgroupRoutes() {
	s.mux.HandleFunc("GET /v1/workgroups", s.handleListWorkgroups)
	s.mux.HandleFunc("POST /v1/workgroups", s.handleCreateWorkgroup)
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
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	out, err := s.control.CreateWorkgroup(r.Context(), body.DisplayName)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
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
		body["allow_tool_names"] = []string{"read_file", "glob_files", "write_file"}
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "invalid json", nil)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeAPIError(w, http.StatusBadRequest, "schema_mismatch", "text required", nil)
		return
	}
	ev, err := s.control.PostWorkgroupMessage(r.Context(), wid, body.Text, body.ClientMessageID)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}
