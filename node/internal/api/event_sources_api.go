package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/events"
)

func (s *Server) registerEventSourceRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/event-sources", s.handleListEventSources)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/event-sources", s.handleRegisterEventSource)
	s.mux.HandleFunc("DELETE /v1/agents/{agent_id}/event-sources/{source_id}", s.handleDeleteEventSource)
}

type eventSourceStateView struct {
	SourceID     string    `json:"source_id,omitempty"`
	OwnerAgentID string    `json:"owner_agent_id,omitempty"`
	Revision     int64     `json:"revision"`
	Digest       string    `json:"digest,omitempty"`
	Cursor       string    `json:"cursor,omitempty"`
	FailureCount int       `json:"failure_count"`
	NextRetryAt  time.Time `json:"next_retry_at,omitempty"`
	Baseline     bool      `json:"baseline"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
}
type eventSourceView struct {
	SourceID     string               `json:"source_id"`
	OwnerAgentID string               `json:"owner_agent_id"`
	Revision     int64                `json:"revision"`
	Root         string               `json:"root"`
	MaxFiles     int                  `json:"max_files"`
	MaxBytes     int                  `json:"max_bytes"`
	HashContent  bool                 `json:"hash_content"`
	Timeout      time.Duration        `json:"timeout"`
	Enabled      bool                 `json:"enabled"`
	State        eventSourceStateView `json:"state"`
}

func eventSourceDTO(reg events.SourceRegistration, st events.State) eventSourceView {
	return eventSourceView{reg.SourceID, reg.OwnerAgentID, reg.Revision, reg.Root, reg.MaxFiles, reg.MaxBytes, reg.HashContent, reg.Timeout, reg.Enabled, eventSourceStateView{st.SourceID, st.OwnerAgentID, st.Revision, st.Digest, st.Cursor, st.FailureCount, st.NextRetryAt, st.Baseline, st.UpdatedAt, st.LastError}}
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
	views := []eventSourceView{}
	for _, reg := range s.eventStore.ListRegistrationsForOwner(owner) {
		st, _ := s.eventStore.Get(reg.SourceID)
		st.PendingEvent = nil
		views = append(views, eventSourceDTO(reg, st))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(views)
}

func (s *Server) handleRegisterEventSource(w http.ResponseWriter, r *http.Request) {
	if s.eventStore == nil {
		http.Error(w, "event store unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		SourceID           string        `json:"source_id"`
		LegacySourceID     string        `json:"SourceID"`
		OwnerAgentID       string        `json:"owner_agent_id"`
		LegacyOwnerAgentID string        `json:"OwnerAgentID"`
		Revision           int64         `json:"revision"`
		Root               string        `json:"root"`
		MaxFiles           *int          `json:"max_files"`
		LegacyMaxFiles     *int          `json:"MaxFiles"`
		MaxBytes           *int          `json:"max_bytes"`
		LegacyMaxBytes     *int          `json:"MaxBytes"`
		HashContent        *bool         `json:"hash_content"`
		LegacyHashContent  *bool         `json:"HashContent"`
		Timeout            time.Duration `json:"timeout"`
		Enabled            *bool         `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.SourceID == "" {
		body.SourceID = body.LegacySourceID
	}
	if body.OwnerAgentID == "" {
		body.OwnerAgentID = body.LegacyOwnerAgentID
	}
	maxFiles, maxBytes := 0, 0
	if body.MaxFiles != nil {
		maxFiles = *body.MaxFiles
	} else if body.LegacyMaxFiles != nil {
		maxFiles = *body.LegacyMaxFiles
	}
	if body.MaxBytes != nil {
		maxBytes = *body.MaxBytes
	} else if body.LegacyMaxBytes != nil {
		maxBytes = *body.LegacyMaxBytes
	}
	hashContent, enabled := false, false
	if body.HashContent != nil {
		hashContent = *body.HashContent
	} else if body.LegacyHashContent != nil {
		hashContent = *body.LegacyHashContent
	}
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	reg := events.SourceRegistration{SourceID: body.SourceID, OwnerAgentID: body.OwnerAgentID, Revision: body.Revision, Root: body.Root, MaxFiles: maxFiles, MaxBytes: maxBytes, HashContent: hashContent, Timeout: body.Timeout, Enabled: enabled}
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(eventSourceDTO(reg, events.State{}))
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
