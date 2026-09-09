package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/events"
)

func (s *Server) registerEventSourceRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/event-sources", s.handleListEventSources)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/event-sources", s.handleRegisterEventSource)
	s.mux.HandleFunc("DELETE /v1/agents/{agent_id}/event-sources/{source_id}", s.handleDeleteEventSource)
}

func (s *Server) eventOwner(r *http.Request) (string, error) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if id == "" || s.agents == nil {
		return "", http.ErrNoCookie
	}
	rec, err := s.agents.Get(context.Background(), id)
	if err != nil || rec == nil || rec.Archived {
		return "", http.ErrNoCookie
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil || snap.AgentType != "auto" {
		return "", http.ErrNoCookie
	}
	return id, nil
}

func (s *Server) handleListEventSources(w http.ResponseWriter, r *http.Request) {
	if s.eventStore == nil {
		http.Error(w, "event store unavailable", http.StatusServiceUnavailable)
		return
	}
	// Registrations intentionally have no secret-bearing fields and are returned
	// only as the durable provider configuration.
	owner, err := s.eventOwner(r)
	if err != nil {
		http.Error(w, "agent not found or not auto", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(s.eventStore.ListRegistrationsForOwner(owner))
}

func (s *Server) handleRegisterEventSource(w http.ResponseWriter, r *http.Request) {
	if s.eventStore == nil {
		http.Error(w, "event store unavailable", http.StatusServiceUnavailable)
		return
	}
	var reg events.SourceRegistration
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	owner, err := s.eventOwner(r)
	if err != nil {
		http.Error(w, "agent not found or not auto", http.StatusNotFound)
		return
	}
	if reg.OwnerAgentID != "" && reg.OwnerAgentID != owner {
		http.Error(w, "event source owner mismatch", http.StatusForbidden)
		return
	}
	reg.OwnerAgentID = owner
	if err := s.eventStore.RegisterSource(reg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(reg)
}

func (s *Server) handleDeleteEventSource(w http.ResponseWriter, r *http.Request) {
	if s.eventStore == nil {
		http.Error(w, "event store unavailable", http.StatusServiceUnavailable)
		return
	}
	id := strings.TrimSpace(r.PathValue("source_id"))
	if id == "" {
		http.Error(w, "source_id required", http.StatusBadRequest)
		return
	}
	owner, err := s.eventOwner(r)
	if err != nil {
		http.Error(w, "agent not found or not auto", http.StatusNotFound)
		return
	}
	rev := int64(0)
	if raw := r.URL.Query().Get("revision"); raw != "" {
		rev, _ = strconv.ParseInt(raw, 10, 64)
	}
	if err := s.eventStore.RemoveRegistrationCAS(id, owner, rev); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
