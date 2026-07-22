package api

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
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

type longTermEntryView struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

type agentPromptContextView struct {
	AgentID               string              `json:"agent_id"`
	SoulMD                string              `json:"soul_md"`
	UserMD                string              `json:"user_md"`
	CustomMD              string              `json:"custom_md"`
	LongTermMD            string              `json:"long_term_md"`
	LongTermScope         string              `json:"long_term_scope"`
	LongTermEntries       []longTermEntryView `json:"long_term_entries"`
	GlobalLongTermEntries []longTermEntryView `json:"global_long_term_entries"`
	Source                string              `json:"source"`
}

type agentPromptContextPutBody struct {
	SoulMD          *string              `json:"soul_md"`
	UserMD          *string              `json:"user_md"`
	CustomMD        *string              `json:"custom_md"`
	LongTermMD      *string              `json:"long_term_md"`
	LongTermScope   *string              `json:"long_term_scope"`
	LongTermEntries *[]longTermEntryView `json:"long_term_entries"`
}

func (s *Server) handleGetAgentPromptContext(w http.ResponseWriter, r *http.Request) {
	id, rec, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	pc, err := s.agents.EnsureAgentPromptContext(r.Context(), id, s.runtimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_load_failed", err.Error(), map[string]any{"agent_id": id})
		return
	}
	view, err := s.buildAgentPromptContextView(r.Context(), id, rec, pc)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_load_failed", err.Error(), map[string]any{"agent_id": id})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handlePutAgentPromptContext(w http.ResponseWriter, r *http.Request) {
	id, rec, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	var body agentPromptContextPutBody
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	pc, err := s.agents.EnsureAgentPromptContext(r.Context(), id, s.runtimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_load_failed", err.Error(), nil)
		return
	}
	if body.SoulMD != nil {
		pc.SoulMD = *body.SoulMD
	}
	if body.UserMD != nil {
		pc.UserMD = *body.UserMD
	}
	if body.CustomMD != nil {
		pc.CustomMD = *body.CustomMD
	}
	if body.LongTermMD != nil {
		pc.LongTermMD = *body.LongTermMD
	}
	pc.AgentID = id
	if err := s.agents.SaveAgentPromptContext(r.Context(), *pc); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_save_failed", err.Error(), nil)
		return
	}
	scope := agentruntime.LongTermScopeFromDefaults(mustParseAgentSnapshot(rec))
	if body.LongTermScope != nil {
		scope = normalizePromptLongTermScope(*body.LongTermScope)
	}
	if body.LongTermEntries != nil {
		entries := longTermViewsToEntries(*body.LongTermEntries)
		ltRec := store.LongTermRecord{
			Scope:   scope,
			AgentID: id,
			Entries: entries,
			UpdatedAt: time.Now().UTC(),
		}
		if err := s.agents.SaveLongTermRecordOverwrite(r.Context(), ltRec); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "longterm_save_failed", err.Error(), nil)
			return
		}
	} else if body.LongTermMD != nil {
		entries := store.EntriesFromLegacyMarkdown(*body.LongTermMD, time.Now().UTC())
		ltRec := store.LongTermRecord{
			Scope:     scope,
			AgentID:   id,
			Entries:   entries,
			UpdatedAt: time.Now().UTC(),
		}
		if err := s.agents.SaveLongTermRecordOverwrite(r.Context(), ltRec); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "longterm_save_failed", err.Error(), nil)
			return
		}
	}
	view, err := s.buildAgentPromptContextView(r.Context(), id, rec, pc)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_load_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"agent_id":       id,
		"prompt_context": view,
	})
}

func (s *Server) buildAgentPromptContextView(ctx context.Context, id string, agentRec *store.AgentRecord, pc *store.AgentPromptContextRecord) (agentPromptContextView, error) {
	scope := agentruntime.LongTermScopeFromDefaults(mustParseAgentSnapshot(agentRec))
	agentLT, err := s.agents.EnsureLongTermRecord(ctx, store.LongTermScopeAgent, id, s.runtimeDir(), pc.LongTermMD)
	if err != nil {
		return agentPromptContextView{}, err
	}
	globalLT, err := s.agents.EnsureLongTermRecord(ctx, store.LongTermScopeGlobal, "", s.runtimeDir(), "")
	if err != nil {
		return agentPromptContextView{}, err
	}
	active := agentLT
	if scope == store.LongTermScopeGlobal {
		active = globalLT
	}
	return agentPromptContextView{
		AgentID:               id,
		SoulMD:                pc.SoulMD,
		UserMD:                pc.UserMD,
		CustomMD:              pc.CustomMD,
		LongTermMD:            turn.FormatLongTermEntries(storeEntriesToTurn(active.Entries)),
		LongTermScope:         scope,
		LongTermEntries:       longTermEntriesToViews(agentLT.Entries),
		GlobalLongTermEntries: longTermEntriesToViews(globalLT.Entries),
		Source:                "sqlite",
	}, nil
}

func mustParseAgentSnapshot(rec *store.AgentRecord) agentruntime.Snapshot {
	if rec == nil {
		return agentruntime.Snapshot{}
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return agentruntime.Snapshot{}
	}
	return snap
}

func normalizePromptLongTermScope(scope string) string {
	if strings.TrimSpace(scope) == store.LongTermScopeGlobal {
		return store.LongTermScopeGlobal
	}
	return store.LongTermScopeAgent
}

func longTermEntriesToViews(entries []store.LongTermEntry) []longTermEntryView {
	out := make([]longTermEntryView, 0, len(entries))
	for _, e := range entries {
		content := strings.TrimSpace(e.Content)
		if content == "" {
			continue
		}
		out = append(out, longTermEntryView{ID: strings.TrimSpace(e.ID), Content: content})
	}
	return out
}

func longTermViewsToEntries(views []longTermEntryView) []store.LongTermEntry {
	now := time.Now().UTC()
	out := make([]store.LongTermEntry, 0, len(views))
	for _, v := range views {
		content := strings.TrimSpace(v.Content)
		if content == "" {
			continue
		}
		id := strings.TrimSpace(v.ID)
		if id == "" {
			out = append(out, store.NewLongTermEntry(content, now))
			continue
		}
		out = append(out, store.LongTermEntry{ID: id, Content: content, CreatedAt: now, UpdatedAt: now})
	}
	return out
}

func promptContentFromRecord(rec *store.AgentPromptContextRecord) *promptcontext.Content {
	if rec == nil {
		return nil
	}
	return &promptcontext.Content{
		Soul:   rec.SoulMD,
		User:   rec.UserMD,
		Custom: rec.CustomMD,
		// LongTerm 由 ReloadLongTermMemory 在清空上下文 / 首条交互 / 压缩完成后加载。
	}
}
