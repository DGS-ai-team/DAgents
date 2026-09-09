package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
)

func (s *Server) registerAutonomyV2Routes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/auto-config", s.handleAutonomyV2ConfigGet)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/auto-config", s.handleAutonomyV2ConfigPut)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/todos", s.handleAutonomyV2TodosGet)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/todos", s.handleAutonomyV2TodoPost)
	s.mux.HandleFunc("PATCH /v1/agents/{agent_id}/todos/{todo_id}", s.handleAutonomyV2TodoPatch)
	s.mux.HandleFunc("DELETE /v1/agents/{agent_id}/todos/{todo_id}", s.handleAutonomyV2TodoDelete)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/experience", s.handleAutonomyV2ExperienceGet)
}

func (s *Server) autonomyV2Agent(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return "", false
	}
	if s.autonomyStore == nil {
		writeAPIError(w, 503, "autonomy_unavailable", "autonomy store unavailable", nil)
		return "", false
	}
	return id, true
}

func decodeAutonomyJSON(r *http.Request, dst any) error {
	b, readErr := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if readErr != nil {
		return readErr
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("request body too large")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON")
		}
		return err
	}
	return nil
}

func (s *Server) handleAutonomyV2ConfigGet(w http.ResponseWriter, r *http.Request) {
	id, ok := s.autonomyV2Agent(w, r)
	if !ok {
		return
	}
	p, exists := s.autonomyStore.GetProfile(id)
	if !exists {
		writeJSON(w, http.StatusOK, autonomy.Profile{AgentID: id, Responsibility: "", WakeIntervalSeconds: 0, MaxToolRounds: 32, DreamingEnabled: false, DreamingTime: "03:00", Timezone: "Asia/Shanghai"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleAutonomyV2ConfigPut(w http.ResponseWriter, r *http.Request) {
	id, ok := s.autonomyV2Agent(w, r)
	if !ok {
		return
	}
	var in struct {
		ExpectedRevision    int64  `json:"expected_revision"`
		Responsibility      string `json:"responsibility"`
		WakeIntervalSeconds int64  `json:"wake_interval_seconds"`
		MaxToolRounds       int    `json:"max_tool_rounds"`
		DreamingEnabled     bool   `json:"dreaming_enabled"`
		DreamingTime        string `json:"dreaming_time"`
		Timezone            string `json:"timezone"`
	}
	if err := decodeAutonomyJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_config", err.Error(), nil)
		return
	}
	p := autonomy.Profile{AgentID: id, Responsibility: in.Responsibility, WakeIntervalSeconds: in.WakeIntervalSeconds, MaxToolRounds: in.MaxToolRounds, DreamingEnabled: in.DreamingEnabled, DreamingTime: in.DreamingTime, Timezone: in.Timezone}
	if err := s.autonomyStore.PutProfile(p, in.ExpectedRevision); err != nil {
		writeAPIError(w, statusForAutonomyError(err), "config_update_failed", err.Error(), nil)
		return
	}
	updated, _ := s.autonomyStore.GetProfile(id)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleAutonomyV2TodosGet(w http.ResponseWriter, r *http.Request) {
	id, ok := s.autonomyV2Agent(w, r)
	if ok {
		writeJSON(w, 200, map[string]any{"todos": s.autonomyStore.ListTodos(id)})
	}
}
func (s *Server) handleAutonomyV2TodoPost(w http.ResponseWriter, r *http.Request) {
	id, ok := s.autonomyV2Agent(w, r)
	if !ok {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decodeAutonomyJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_todo", err.Error(), nil)
		return
	}
	t, err := s.autonomyStore.CreateTodo(id, in.Text)
	if err != nil {
		writeAPIError(w, 400, "invalid_todo", err.Error(), nil)
		return
	}
	writeJSON(w, 201, t)
}
func (s *Server) handleAutonomyV2TodoPatch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.autonomyV2Agent(w, r)
	if !ok {
		return
	}
	var in struct {
		ExpectedRevision int64  `json:"expected_revision"`
		Text             string `json:"text"`
		Status           string `json:"status"`
	}
	if err := decodeAutonomyJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_todo", err.Error(), nil)
		return
	}
	t, err := s.autonomyStore.UpdateTodoCAS(id, r.PathValue("todo_id"), in.ExpectedRevision, in.Text, in.Status)
	if err != nil {
		writeAPIError(w, statusForAutonomyError(err), "todo_update_failed", err.Error(), nil)
		return
	}
	writeJSON(w, 200, t)
}
func (s *Server) handleAutonomyV2TodoDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.autonomyV2Agent(w, r)
	if !ok {
		return
	}
	var in struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := decodeAutonomyJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_todo", err.Error(), nil)
		return
	}
	if err := s.autonomyStore.DeleteTodoCAS(id, r.PathValue("todo_id"), in.ExpectedRevision); err != nil {
		writeAPIError(w, statusForAutonomyError(err), "todo_delete_failed", err.Error(), nil)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true})
}
func (s *Server) handleAutonomyV2ExperienceGet(w http.ResponseWriter, r *http.Request) {
	id, ok := s.autonomyV2Agent(w, r)
	if !ok {
		return
	}
	e, exists := s.autonomyStore.GetExperience(id)
	if !exists {
		writeJSON(w, 200, map[string]any{"experience": nil})
		return
	}
	writeJSON(w, 200, e)
}

func statusForAutonomyError(err error) int {
	if err == autonomy.ErrNotFound {
		return 404
	}
	if err == autonomy.ErrConflict {
		return 409
	}
	return 400
}
