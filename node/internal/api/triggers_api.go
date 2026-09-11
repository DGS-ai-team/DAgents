package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func (s *Server) registerTriggerRoutes() {
	s.mux.HandleFunc("POST /v1/triggers", s.handleCreateTrigger)
	s.mux.HandleFunc("GET /v1/triggers", s.handleListTriggers)
	s.mux.HandleFunc("GET /v1/triggers/{trigger_id}", s.handleGetTrigger)
	s.mux.HandleFunc("PATCH /v1/triggers/{trigger_id}", s.handleUpdateTrigger)
	s.mux.HandleFunc("DELETE /v1/triggers/{trigger_id}", s.handleDeleteTrigger)
	s.mux.HandleFunc("POST /v1/triggers/{trigger_id}/fire", s.handleFireTrigger)
	s.mux.HandleFunc("GET /v1/triggers/{trigger_id}/history", s.handleTriggerHistory)
	s.mux.HandleFunc("POST /v1/triggers/{trigger_id}/recover", s.handleRecoverTrigger)
}

func (s *Server) handleRecoverTrigger(w http.ResponseWriter, r *http.Request) {
	store := s.requireTriggerStore(w)
	if store == nil {
		return
	}
	var body struct {
		DeliveryID string `json:"delivery_id"`
		Revision   int64  `json:"revision"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("trigger_id"))
	if err := store.RecoverAuthorized(triggers.Principal{Kind: "admin", ID: "local"}, id, body.Revision, body.DeliveryID); err != nil {
		writeAPIError(w, http.StatusConflict, "recovery_required", err.Error(), map[string]any{"trigger_id": id})
		return
	}
	def, _ := store.GetTrigger(id)
	writeJSON(w, http.StatusOK, def)
}

func (s *Server) requireTriggerStore(w http.ResponseWriter) *triggers.Store {
	if s.triggerStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "triggers_unavailable", "trigger store 未初始化", nil)
		return nil
	}
	return s.triggerStore
}

func (s *Server) handleCreateTrigger(w http.ResponseWriter, r *http.Request) {
	store := s.requireTriggerStore(w)
	if store == nil {
		return
	}
	var body triggers.CreateInput
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if id := strings.TrimSpace(body.TargetAgentID); id != "" && s.agents != nil {
		rec, err := s.agents.Get(r.Context(), id)
		if err != nil || rec == nil || rec.Archived {
			writeAPIError(w, http.StatusBadRequest, "agent_not_found", "target agent not found or archived", map[string]any{"agent_id": id})
			return
		}
	}
	if body.SessionTargetMode != "" && body.SessionTargetMode != triggers.SessionTargetFixed {
		writeAPIError(w, http.StatusBadRequest, "unsupported_session_target_mode", "P0 supports only fixed session targets", nil)
		return
	}
	created, err := store.CreateAuthorized(triggers.Principal{Kind: "admin", ID: "local"}, body, time.Now())
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "create_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, created)
}

func (s *Server) handleListTriggers(w http.ResponseWriter, _ *http.Request) {
	store := s.requireTriggerStore(w)
	if store == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"triggers": store.ListAuthorized(triggers.Principal{Kind: "admin", ID: "local"})})
}

func (s *Server) handleGetTrigger(w http.ResponseWriter, r *http.Request) {
	store := s.requireTriggerStore(w)
	if store == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("trigger_id"))
	def, err := store.GetAuthorized(triggers.Principal{Kind: "admin", ID: "local"}, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "trigger not found", map[string]any{"trigger_id": id})
		return
	}
	writeJSON(w, http.StatusOK, def)
}

func (s *Server) handleUpdateTrigger(w http.ResponseWriter, r *http.Request) {
	store := s.requireTriggerStore(w)
	if store == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("trigger_id"))
	current, authErr := store.GetAuthorized(triggers.Principal{Kind: "admin", ID: "local"}, id)
	if authErr != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "trigger not found", nil)
		return
	}
	if current.Controller != "user" {
		writeAPIError(w, 409, "system_managed_trigger", "system-managed triggers cannot be edited here", nil)
		return
	}
	var patch triggers.UpdatePatch
	if err := decodeJSON(r, &patch); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if patch.Revision != nil && *patch.Revision <= 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_revision", "revision must be positive", nil)
		return
	}
	if patch.TargetAgentID != nil && s.agents != nil {
		id := strings.TrimSpace(*patch.TargetAgentID)
		rec, err := s.agents.Get(r.Context(), id)
		if id == "" || err != nil || rec == nil || rec.Archived {
			writeAPIError(w, http.StatusBadRequest, "agent_not_found", "target agent not found or archived", map[string]any{"agent_id": id})
			return
		}
	}
	if patch.SessionTargetMode != nil && *patch.SessionTargetMode != triggers.SessionTargetFixed {
		writeAPIError(w, http.StatusBadRequest, "unsupported_session_target_mode", "P0 supports only fixed session targets", nil)
		return
	}
	expected := current.Revision
	if patch.Revision != nil {
		expected = *patch.Revision
	}
	patch.Revision = nil
	updated, err := store.UpdateAuthorized(triggers.Principal{Kind: "admin", ID: "local"}, id, expected, patch, time.Now())
	if triggers.IsNotFound(err) {
		writeAPIError(w, http.StatusNotFound, "not_found", "trigger not found", map[string]any{"trigger_id": id})
		return
	}
	if errors.Is(err, triggers.ErrRevisionConflict) {
		writeAPIError(w, http.StatusConflict, "revision_conflict", err.Error(), nil)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "update_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteTrigger(w http.ResponseWriter, r *http.Request) {
	store := s.requireTriggerStore(w)
	if store == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("trigger_id"))
	current, authErr := store.GetAuthorized(triggers.Principal{Kind: "admin", ID: "local"}, id)
	if authErr != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "trigger not found", nil)
		return
	}
	if current.Controller != "user" {
		writeAPIError(w, 409, "system_managed_trigger", "system-managed triggers cannot be edited here", nil)
		return
	}
	expected := current.Revision
	if raw := r.URL.Query().Get("revision"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_revision", "revision must be positive", nil)
			return
		}
		expected = parsed
	}
	if raw := r.Header.Get("If-Match"); raw != "" {
		parsed, err := strconv.ParseInt(strings.Trim(raw, "\""), 10, 64)
		if err != nil || parsed <= 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_revision", "revision must be positive", nil)
			return
		}
		expected = parsed
	}
	if err := store.DeleteAuthorized(triggers.Principal{Kind: "admin", ID: "local"}, id, expected); err != nil {
		stable := "delete_failed"
		if errors.Is(err, triggers.ErrRevisionConflict) {
			stable = "revision_conflict"
		}
		writeAPIError(w, http.StatusConflict, stable, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"trigger_id": id,
		"deleted":    true,
	})
}

type triggerFireRequest struct {
	Reason   string         `json:"reason"`
	Payload  map[string]any `json:"payload"`
	Force    bool           `json:"force"`
	Revision int64          `json:"revision"`
}

func (s *Server) handleFireTrigger(w http.ResponseWriter, r *http.Request) {
	if s.triggerSched == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "scheduler_disabled", "trigger scheduler is disabled", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("trigger_id"))
	var body triggerFireRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "manual"
	}
	record, err := s.triggerSched.FireAuthorized(triggers.Principal{Kind: "admin", ID: "local"}, id, body.Revision, reason, body.Payload, body.Force, nil)
	if triggers.IsNotFound(err) {
		writeAPIError(w, http.StatusNotFound, "not_found", "trigger not found", map[string]any{"trigger_id": id})
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "fire_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleTriggerHistory(w http.ResponseWriter, r *http.Request) {
	store := s.requireTriggerStore(w)
	if store == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("trigger_id"))
	records, err := store.HistoryAuthorized(triggers.Principal{Kind: "admin", ID: "local"}, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "trigger not found", map[string]any{"trigger_id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records})
}
