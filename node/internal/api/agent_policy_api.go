package api

import (
	"net/http"
	"runtime"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func (s *Server) registerAgentPolicyRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/policy", s.handleGetAgentPolicy)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/policy/tools", s.handlePutAgentToolPolicy)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/policy/shell/{shell_type}", s.handlePutAgentShellPolicy)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/prompt-context", s.handleGetAgentPromptContext)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/prompt-context", s.handlePutAgentPromptContext)
}

func (s *Server) requireAgentRecord(w http.ResponseWriter, r *http.Request) (string, *store.AgentRecord, bool) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return "", nil, false
	}
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return "", nil, false
	}
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_lookup_failed", err.Error(), map[string]any{"agent_id": id})
		return "", nil, false
	}
	if rec == nil || rec.Archived {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
		return "", nil, false
	}
	return id, rec, true
}

func (s *Server) runtimeDir() string {
	if s.cfg == nil {
		return ""
	}
	return s.cfg.RuntimeDir()
}

func (s *Server) handleGetAgentPolicy(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	engine, err := s.agents.LoadAgentPolicyEngine(r.Context(), id, s.runtimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_load_failed", err.Error(), map[string]any{"agent_id": id})
		return
	}
	toolNames := []string{}
	if s.sessions != nil {
		toolNames = s.sessions.ToolNames()
	}
	snap, err := policy.LoadSnapshotForAgent(id, "", "sqlite", engine, toolNames)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_snapshot_failed", err.Error(), nil)
		return
	}
	snap.Platform.GOOS = runtime.GOOS
	defaultShell, _ := policy.ResolveShellType(nil)
	snap.Platform.DefaultShell = string(defaultShell)

	shellQuery := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("shell")))
	if shellQuery != "" && shellQuery != "auto" {
		st, err := policy.ParseShellTypeParam(shellQuery)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_shell", err.Error(), nil)
			return
		}
		key := string(st)
		filtered := snap.Shell[key]
		snap.Shell = map[string][]policy.ShellPolicyEntry{key: filtered}
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handlePutAgentToolPolicy(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	var body policyToolUpdatesBody
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if len(body.Updates) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_updates", "updates is required", nil)
		return
	}
	rec, err := s.agents.EnsureAgentPolicy(r.Context(), id, s.runtimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_load_failed", err.Error(), nil)
		return
	}
	maps := policy.StringMapsToMaps(rec.Tools, rec.Shell)
	maps, err = policy.ApplyToolUpdatesToMaps(maps, body.Updates)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "policy_update_failed", err.Error(), nil)
		return
	}
	tools, shell := policy.MapsToStringMaps(maps)
	if err := s.agents.SaveAgentPolicy(r.Context(), store.AgentPolicyRecord{
		AgentID: id,
		Tools:   tools,
		Shell:   shell,
	}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_save_failed", err.Error(), nil)
		return
	}
	engine := policy.NewEngineFromMaps(maps)
	if s.sessions != nil {
		s.sessions.SetSessionPolicy(id, engine)
	}
	s.logger.Info("agent policy tools updated", "agent_id", id, "count", len(body.Updates))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id})
}

func (s *Server) handlePutAgentShellPolicy(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	shellRaw := strings.TrimSpace(r.PathValue("shell_type"))
	shellType, err := policy.ParseShellTypeParam(shellRaw)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_shell", err.Error(), nil)
		return
	}
	var body policyShellUpdatesBody
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if len(body.Updates) == 0 && len(body.Deletes) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_updates", "updates or deletes is required", nil)
		return
	}
	rec, err := s.agents.EnsureAgentPolicy(r.Context(), id, s.runtimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_load_failed", err.Error(), nil)
		return
	}
	maps := policy.StringMapsToMaps(rec.Tools, rec.Shell)
	maps, err = policy.ApplyShellPolicyChangesToMaps(maps, shellType, body.Updates, body.Deletes)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "policy_update_failed", err.Error(), nil)
		return
	}
	tools, shell := policy.MapsToStringMaps(maps)
	if err := s.agents.SaveAgentPolicy(r.Context(), store.AgentPolicyRecord{
		AgentID: id,
		Tools:   tools,
		Shell:   shell,
	}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_save_failed", err.Error(), nil)
		return
	}
	engine := policy.NewEngineFromMaps(maps)
	if s.sessions != nil {
		s.sessions.SetSessionPolicy(id, engine)
	}
	s.logger.Info("agent policy shell updated", "agent_id", id, "shell", shellType, "updates", len(body.Updates), "deletes", len(body.Deletes))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id})
}

type agentPromptContextView struct {
	AgentID    string `json:"agent_id"`
	SoulMD     string `json:"soul_md"`
	UserMD     string `json:"user_md"`
	CustomMD   string `json:"custom_md"`
	LongTermMD string `json:"long_term_md"`
	Source     string `json:"source"`
}

type agentPromptContextPutBody struct {
	SoulMD     *string `json:"soul_md"`
	UserMD     *string `json:"user_md"`
	CustomMD   *string `json:"custom_md"`
	LongTermMD *string `json:"long_term_md"`
}

func (s *Server) handleGetAgentPromptContext(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	rec, err := s.agents.EnsureAgentPromptContext(r.Context(), id, s.runtimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_load_failed", err.Error(), map[string]any{"agent_id": id})
		return
	}
	writeJSON(w, http.StatusOK, agentPromptContextView{
		AgentID:    id,
		SoulMD:     rec.SoulMD,
		UserMD:     rec.UserMD,
		CustomMD:   rec.CustomMD,
		LongTermMD: rec.LongTermMD,
		Source:     "sqlite",
	})
}

func (s *Server) handlePutAgentPromptContext(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	var body agentPromptContextPutBody
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	rec, err := s.agents.EnsureAgentPromptContext(r.Context(), id, s.runtimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_load_failed", err.Error(), nil)
		return
	}
	if body.SoulMD != nil {
		rec.SoulMD = *body.SoulMD
	}
	if body.UserMD != nil {
		rec.UserMD = *body.UserMD
	}
	if body.CustomMD != nil {
		rec.CustomMD = *body.CustomMD
	}
	if body.LongTermMD != nil {
		rec.LongTermMD = *body.LongTermMD
	}
	rec.AgentID = id
	if err := s.agents.SaveAgentPromptContext(r.Context(), *rec); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_save_failed", err.Error(), nil)
		return
	}
	// 侧车正文变更需重建 runtime 才能注入；提示客户端可 reload。
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"agent_id": id,
		"prompt_context": agentPromptContextView{
			AgentID:    id,
			SoulMD:     rec.SoulMD,
			UserMD:     rec.UserMD,
			CustomMD:   rec.CustomMD,
			LongTermMD: rec.LongTermMD,
			Source:     "sqlite",
		},
	})
}

func promptContentFromRecord(rec *store.AgentPromptContextRecord) *promptcontext.Content {
	if rec == nil {
		return nil
	}
	return &promptcontext.Content{
		Soul:     rec.SoulMD,
		User:     rec.UserMD,
		Custom:   rec.CustomMD,
		LongTerm: rec.LongTermMD,
	}
}
