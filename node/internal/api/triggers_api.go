package api

import (
	"net/http"
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
	def, err := triggers.NewDefinitionFromCreate(body, s.cfg.NodeID, time.Now())
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_trigger", err.Error(), nil)
		return
	}
	created, err := store.CreateTrigger(def)
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
	writeJSON(w, http.StatusOK, map[string]any{"triggers": store.ListTriggers()})
}

func (s *Server) handleGetTrigger(w http.ResponseWriter, r *http.Request) {
	store := s.requireTriggerStore(w)
	if store == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("trigger_id"))
	def, ok := store.GetTrigger(id)
	if !ok {
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
	var patch triggers.UpdatePatch
	if err := decodeJSON(r, &patch); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	updated, err := store.UpdateTrigger(id, patch, time.Now())
	if triggers.IsNotFound(err) {
		writeAPIError(w, http.StatusNotFound, "not_found", "trigger not found", map[string]any{"trigger_id": id})
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
	writeJSON(w, http.StatusOK, map[string]any{
		"trigger_id": id,
		"deleted":    store.DeleteTrigger(id),
	})
}

type triggerFireRequest struct {
	Reason  string         `json:"reason"`
	Payload map[string]any `json:"payload"`
	Force   bool           `json:"force"`
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
	record, err := s.triggerSched.FireTrigger(id, reason, body.Payload, body.Force, nil)
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
	if _, ok := store.GetTrigger(id); !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "trigger not found", map[string]any{"trigger_id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": store.ListHistory(id)})
}
