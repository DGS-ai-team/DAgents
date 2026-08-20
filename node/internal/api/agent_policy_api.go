package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type policyToolUpdatesBody struct {
	Updates []policy.ToolUpdate `json:"updates"`
}

type policyShellUpdatesBody struct {
	Updates []policy.ShellUpdate `json:"updates"`
	Deletes []string             `json:"deletes"`
}

func (s *Server) registerAgentPolicyRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/policy", s.handleGetAgentPolicy)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/policy/tools", s.handlePutAgentToolPolicy)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/policy/shell/{shell_type}", s.handlePutAgentShellPolicy)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/prompt-context", s.handleGetAgentPromptContext)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/prompt-context", s.handlePutAgentPromptContext)
	s.mux.HandleFunc("PATCH /v1/agents/{agent_id}/prompt-context/memory/{entry_id}", s.handlePatchAgentMemoryEntry)
	s.mux.HandleFunc("DELETE /v1/agents/{agent_id}/prompt-context/memory/{entry_id}", s.handleDeleteAgentMemoryEntry)
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
	id, rec, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	engine, err := s.agents.LoadAgentPolicyEngine(r.Context(), id, s.runtimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_load_failed", err.Error(), map[string]any{"agent_id": id})
		return
	}

	// 仅展示该 Agent 已启用工具组内的工具（未启用的组不出现在策略 UI）。
	parsed, _ := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	groups := agentruntime.EnabledToolGroups(parsed)
	enabledNames := config.ExpandBuiltinToolGroups(groups)
	if s.mcpManager != nil {
		enabledNames = append(enabledNames, s.mcpManager.ToolNames(mcp.BindingsFromDefaults(parsed.Defaults))...)
	}
	enabledSet := make(map[string]struct{}, len(enabledNames))
	for _, name := range enabledNames {
		enabledSet[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}

	snap, err := policy.LoadSnapshotForAgent(id, "", "sqlite", engine, enabledNames)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_snapshot_failed", err.Error(), nil)
		return
	}
	if len(enabledSet) == 0 {
		snap.Tools = []policy.ToolPolicyEntry{}
	} else {
		filtered := make([]policy.ToolPolicyEntry, 0, len(snap.Tools))
		for _, entry := range snap.Tools {
			if _, ok := enabledSet[strings.ToLower(strings.TrimSpace(entry.Name))]; ok {
				filtered = append(filtered, entry)
			}
		}
		snap.Tools = filtered
	}
	hasBash := false
	for _, g := range groups {
		if strings.TrimSpace(g) == "bash" {
			hasBash = true
			break
		}
	}
	if !hasBash {
		snap.Shell = map[string][]policy.ShellPolicyEntry{}
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
	s.publishRuntimeConfigChanged(id, "execution_policy", true)
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
	s.publishRuntimeConfigChanged(id, "execution_policy", true)
	s.logger.Info("agent policy shell updated", "agent_id", id, "shell", shellType, "updates", len(body.Updates), "deletes", len(body.Deletes))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id})
}

type longTermEntryView struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
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

type agentMemoryEntryMutationBody struct {
	Scope   string `json:"scope"`
	Content string `json:"content"`
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
	snap := mustParseAgentSnapshot(rec)
	scope := agentruntime.LongTermScopeFromDefaults(snap)
	scopeChanged := false
	if body.LongTermScope != nil {
		nextScope := normalizePromptLongTermScope(*body.LongTermScope)
		scopeChanged = nextScope != scope
		scope = nextScope
	}
	if scopeChanged {
		if parsed, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_invalid", err.Error(), nil)
			return
		} else {
			promptDefaults, _ := parsed.Defaults["prompt_context"].(map[string]any)
			if promptDefaults == nil {
				promptDefaults = make(map[string]any)
			}
			promptDefaults["long_term_scope"] = scope
			parsed.Defaults["prompt_context"] = promptDefaults
			raw, err := json.Marshal(map[string]any{"template_id": parsed.TemplateID, "defaults": parsed.Defaults})
			if err != nil {
				writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_encode_failed", err.Error(), nil)
				return
			}
			rec.ConfigSnapshot = raw
			if err := s.agents.Save(r.Context(), *rec); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "agent_save_failed", err.Error(), nil)
				return
			}
			if updated, err := s.agents.Get(r.Context(), id); err == nil && updated != nil {
				rec = updated
			}
		}
	}
	memoryChanged := false
	memoryCount := 0
	if body.LongTermEntries != nil {
		entries := longTermViewsToEntries(*body.LongTermEntries)
		memoryChanged = true
		memoryCount = len(entries)
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
	} else if body.LongTermMD != nil {
		entries := store.EntriesFromLegacyMarkdown(*body.LongTermMD, time.Now().UTC())
		memoryChanged = true
		memoryCount = len(entries)
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
	if s.sessions != nil {
		content := promptContentFromRecord(pc)
		if content != nil {
			s.sessions.RefreshRuntimePromptContext(id, *content, scope)
		}
	}
	// The live sidecar reader is refreshed here; a scope change additionally
	// bumps the Agent snapshot so the next Turn rebuilds the full runtime.
	s.publishRuntimeConfigChanged(id, "prompt_context", true)
	if memoryChanged {
		s.publishMemoryChanged(id, "prompt_context", memoryCount, true)
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

func (s *Server) handlePatchAgentMemoryEntry(w http.ResponseWriter, r *http.Request) {
	id, rec, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	entryID := strings.TrimSpace(r.PathValue("entry_id"))
	if entryID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_entry", "entry_id is required", nil)
		return
	}
	var body agentMemoryEntryMutationBody
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	scope, err := parseMemoryScope(body.Scope, r.URL.Query().Get("scope"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_scope", err.Error(), nil)
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_content", "content is required", nil)
		return
	}
	updated, err := s.agents.UpdateLongTermEntry(r.Context(), scope, id, entryID, content)
	if err != nil {
		writeMemoryMutationError(w, err)
		return
	}
	if err := s.refreshMemoryRuntime(r, id, rec, len(updated.Entries)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "memory_runtime_refresh_failed", err.Error(), nil)
		return
	}
	s.writeMemoryMutationResponse(w, r, id, rec)
}

func (s *Server) handleDeleteAgentMemoryEntry(w http.ResponseWriter, r *http.Request) {
	id, rec, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	entryID := strings.TrimSpace(r.PathValue("entry_id"))
	if entryID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_entry", "entry_id is required", nil)
		return
	}
	scope, err := parseMemoryScope(r.URL.Query().Get("scope"), "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_scope", err.Error(), nil)
		return
	}
	updated, err := s.agents.DeleteLongTermEntry(r.Context(), scope, id, entryID)
	if err != nil {
		writeMemoryMutationError(w, err)
		return
	}
	if err := s.refreshMemoryRuntime(r, id, rec, len(updated.Entries)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "memory_runtime_refresh_failed", err.Error(), nil)
		return
	}
	s.writeMemoryMutationResponse(w, r, id, rec)
}

func parseMemoryScope(bodyScope, queryScope string) (string, error) {
	scope := strings.TrimSpace(bodyScope)
	if scope == "" {
		scope = strings.TrimSpace(queryScope)
	}
	if scope == "" {
		return store.LongTermScopeAgent, nil
	}
	if scope != store.LongTermScopeAgent && scope != store.LongTermScopeGlobal {
		return "", errors.New("scope must be agent or global")
	}
	return scope, nil
}

func writeMemoryMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrLongTermEntryNotFound) {
		writeAPIError(w, http.StatusNotFound, "memory_entry_not_found", err.Error(), nil)
		return
	}
	if strings.Contains(err.Error(), "content is required") {
		writeAPIError(w, http.StatusBadRequest, "invalid_content", err.Error(), nil)
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "memory_update_failed", err.Error(), nil)
}

func (s *Server) refreshMemoryRuntime(r *http.Request, id string, rec *store.AgentRecord, count int) error {
	pc, err := s.agents.EnsureAgentPromptContext(r.Context(), id, s.runtimeDir())
	if err != nil {
		return err
	}
	if s.sessions != nil {
		content := promptContentFromRecord(pc)
		if content != nil {
			// Editing the non-active scope must not switch the agent's configured
			// memory scope. The edited scope is only the persistence target.
			runtimeScope := agentruntime.LongTermScopeFromDefaults(mustParseAgentSnapshot(rec))
			s.sessions.RefreshRuntimePromptContext(id, *content, runtimeScope)
		}
	}
	s.publishRuntimeConfigChanged(id, "prompt_context", true)
	s.publishMemoryChanged(id, "prompt_context", count, true)
	return nil
}

func (s *Server) writeMemoryMutationResponse(w http.ResponseWriter, r *http.Request, id string, rec *store.AgentRecord) {
	pc, err := s.agents.EnsureAgentPromptContext(r.Context(), id, s.runtimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_load_failed", err.Error(), nil)
		return
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
		out = append(out, longTermEntryView{
			ID:        strings.TrimSpace(e.ID),
			Content:   content,
			CreatedAt: formatLongTermDate(e.CreatedAt),
			UpdatedAt: formatLongTermDate(e.UpdatedAt),
		})
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
		createdAt := parseLongTermDate(v.CreatedAt, now)
		updatedAt := parseLongTermDate(v.UpdatedAt, createdAt)
		out = append(out, store.LongTermEntry{ID: id, Content: content, CreatedAt: createdAt, UpdatedAt: updatedAt})
	}
	return out
}

func formatLongTermDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("20060102")
}

func parseLongTermDate(value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if parsed, err := time.Parse("20060102", value); err == nil {
		return parsed.UTC()
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC()
	}
	return fallback
}

func promptContentFromRecord(rec *store.AgentPromptContextRecord) *promptcontext.Content {
	if rec == nil {
		return nil
	}
	return &promptcontext.Content{
		Soul:   rec.SoulMD,
		Custom: rec.CustomMD,
		// User 侧车已废弃：用户称呼来自 Node PreferredName。
		// LongTerm 由 ReloadLongTermMemory 在清空上下文 / 首条交互 / 压缩完成后加载。
	}
}
