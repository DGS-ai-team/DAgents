package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
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
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/policy/grants", s.handleGetAgentPolicyGrants)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/policy/grants", s.handlePostAgentPolicyGrant)
	s.mux.HandleFunc("DELETE /v1/agents/{agent_id}/policy/grants/{grant_id}", s.handleDeleteAgentPolicyGrant)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/prompt-context", s.handleGetAgentPromptContext)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/prompt-context", s.handlePutAgentPromptContext)
	s.mux.HandleFunc("PATCH /v1/agents/{agent_id}/prompt-context/memory/{entry_id}", s.handlePatchAgentMemoryEntry)
	s.mux.HandleFunc("DELETE /v1/agents/{agent_id}/prompt-context/memory/{entry_id}", s.handleDeleteAgentMemoryEntry)
}

type policyGrantBody struct {
	Tools     []string `json:"tools"`
	Workspace string   `json:"workspace"`
	ExpiresAt string   `json:"expires_at"`
}

var errGrantNotFound = errors.New("grant not found")

func (s *Server) handleGetAgentPolicyGrants(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	rec, err := s.agents.EnsureAgentPolicy(r.Context(), id)
	if err != nil {
		writeAPIError(w, 500, "policy_load_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": id, "grants": rec.Grants})
}

func (s *Server) handlePostAgentPolicyGrant(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	var body policyGrantBody
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, 400, "invalid_json", err.Error(), nil)
		return
	}
	workspace, err := workspacecoord.Canonical(strings.TrimSpace(body.Workspace))
	if err != nil || workspace == "." {
		writeAPIError(w, 400, "invalid_workspace", "workspace is required", nil)
		return
	}
	expires, err := time.Parse(time.RFC3339, strings.TrimSpace(body.ExpiresAt))
	if err != nil || !expires.After(time.Now().UTC()) {
		writeAPIError(w, 400, "invalid_expiry", "expires_at must be a future RFC3339 timestamp", nil)
		return
	}
	tools := make([]string, 0, len(body.Tools))
	seen := map[string]bool{}
	for _, raw := range body.Tools {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name != "" && !policy.GrantToolSupported(name) {
			writeAPIError(w, http.StatusBadRequest, "unsupported_tool", "tool is not eligible for workspace grants", map[string]any{"tool": name})
			return
		}
		if name != "" && !seen[name] {
			seen[name] = true
			tools = append(tools, name)
		}
	}
	if len(tools) == 0 {
		writeAPIError(w, 400, "invalid_tools", "tools is required", nil)
		return
	}
	grant := policy.Grant{ID: fmt.Sprintf("grant-%d", time.Now().UnixNano()), Tools: tools, Workspace: workspace, ExpiresAt: expires.UTC()}
	_, err = s.agents.MutateAgentPolicy(r.Context(), id, func(rec *store.AgentPolicyRecord) error { rec.Grants = append(rec.Grants, grant); return nil })
	if err != nil {
		writeAPIError(w, 500, "policy_save_failed", err.Error(), nil)
		return
	}
	engine, loadErr := s.agents.LoadAgentPolicyEngine(r.Context(), id)
	if loadErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_reload_failed", loadErr.Error(), nil)
		return
	}
	if s.sessions != nil {
		s.sessions.SetAgentPolicy(id, engine)
	}
	writeJSON(w, http.StatusCreated, grant)
}

func (s *Server) handleDeleteAgentPolicyGrant(w http.ResponseWriter, r *http.Request) {
	id, _, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	grantID := strings.TrimSpace(r.PathValue("grant_id"))
	now := time.Now().UTC()
	_, err := s.agents.MutateAgentPolicy(r.Context(), id, func(rec *store.AgentPolicyRecord) error {
		for i := range rec.Grants {
			if rec.Grants[i].ID == grantID && rec.Grants[i].RevokedAt == nil {
				rec.Grants[i].RevokedAt = &now
				return nil
			}
		}
		return errGrantNotFound
	})
	if err != nil {
		if errors.Is(err, errGrantNotFound) {
			writeAPIError(w, http.StatusNotFound, "grant_not_found", "grant not found", nil)
		} else {
			writeAPIError(w, http.StatusInternalServerError, "policy_save_failed", err.Error(), nil)
		}
		return
	}
	engine, reloadErr := s.agents.LoadAgentPolicyEngine(r.Context(), id)
	if reloadErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_reload_failed", reloadErr.Error(), nil)
		return
	}
	if s.sessions != nil {
		s.sessions.SetAgentPolicy(id, engine)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "grant_id": grantID, "revoked_at": now})
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
	engine, err := s.agents.LoadAgentPolicyEngine(r.Context(), id)
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
	var maps policy.Maps
	_, err := s.agents.MutateAgentPolicy(r.Context(), id, func(rec *store.AgentPolicyRecord) error {
		maps = policy.StringMapsToMaps(rec.Tools, rec.Shell)
		maps.Grants = rec.Grants
		var applyErr error
		maps, applyErr = policy.ApplyToolUpdatesToMaps(maps, body.Updates)
		if applyErr != nil {
			return applyErr
		}
		rec.Tools, rec.Shell = policy.MapsToStringMaps(maps)
		return nil
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "policy_update_failed", err.Error(), nil)
		return
	}
	engine := policy.NewEngineFromMaps(maps)
	if s.sessions != nil {
		s.sessions.SetAgentPolicy(id, engine)
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
	var maps policy.Maps
	_, err = s.agents.MutateAgentPolicy(r.Context(), id, func(rec *store.AgentPolicyRecord) error {
		maps = policy.StringMapsToMaps(rec.Tools, rec.Shell)
		maps.Grants = rec.Grants
		var applyErr error
		maps, applyErr = policy.ApplyShellPolicyChangesToMaps(maps, shellType, body.Updates, body.Deletes)
		if applyErr != nil {
			return applyErr
		}
		rec.Tools, rec.Shell = policy.MapsToStringMaps(maps)
		return nil
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "policy_update_failed", err.Error(), nil)
		return
	}
	engine := policy.NewEngineFromMaps(maps)
	if s.sessions != nil {
		s.sessions.SetAgentPolicy(id, engine)
	}
	s.publishRuntimeConfigChanged(id, "execution_policy", true)
	s.logger.Info("agent policy shell updated", "agent_id", id, "shell", shellType, "updates", len(body.Updates), "deletes", len(body.Deletes))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id})
}

type memoryEntryView struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type agentPromptContextView struct {
	AgentID             string            `json:"agent_id"`
	SoulMD              string            `json:"soul_md"`
	CustomMD            string            `json:"custom_md"`
	MemoryScope         string            `json:"memory_scope"`
	MemoryEntries       []memoryEntryView `json:"memory_entries"`
	GlobalMemoryEntries []memoryEntryView `json:"global_memory_entries"`
	Source              string            `json:"source"`
}

type agentPromptContextPutBody struct {
	SoulMD        *string            `json:"soul_md"`
	CustomMD      *string            `json:"custom_md"`
	MemoryScope   *string            `json:"memory_scope"`
	MemoryEntries *[]memoryEntryView `json:"memory_entries"`
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
	pc, err := s.agents.EnsureAgentPromptContext(r.Context(), id)
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
	pc, err := s.agents.EnsureAgentPromptContext(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_load_failed", err.Error(), nil)
		return
	}
	snap := mustParseAgentSnapshot(rec)
	scope := agentruntime.MemoryScopeFromDefaults(snap)
	scopeChanged := false
	if body.MemoryScope != nil {
		nextScope := normalizeMemoryScope(*body.MemoryScope)
		scopeChanged = nextScope != scope
		scope = nextScope
	}
	var memoryService *memory.LocalService
	if opened, openErr := s.openAgentMemoryService(id, rec); openErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "memory_store_open_failed", openErr.Error(), nil)
		return
	} else {
		memoryService = opened
	}
	if memoryService != nil {
		defer memoryService.Close()
	}
	if body.SoulMD != nil {
		pc.SoulMD = *body.SoulMD
	}
	if body.CustomMD != nil {
		pc.CustomMD = *body.CustomMD
	}
	pc.AgentID = id
	if err := s.agents.SaveAgentPromptContext(r.Context(), *pc); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prompt_context_save_failed", err.Error(), nil)
		return
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
			promptDefaults["memory_scope"] = scope
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
	if body.MemoryEntries != nil {
		memoryChanged = true
		entries := memoryViewsToEntries(*body.MemoryEntries, memory.Scope(scope))
		memoryCount = len(entries)
		if err := memoryService.ReplaceAll(r.Context(), memory.Scope(scope), entries); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "memory_save_failed", err.Error(), nil)
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
	var count int
	if service, openErr := s.openAgentMemoryService(id, rec); openErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "memory_store_open_failed", openErr.Error(), nil)
		return
	} else {
		defer service.Close()
		// Settings is an explicit control-plane operation and may edit either
		// projection, even when the Agent's model scope is the other one.
		if _, err := service.UpdateContent(r.Context(), memory.Scope(scope), entryID, content); err != nil {
			writeMemoryMutationError(w, err)
			return
		}
		entries, listErr := service.List(r.Context(), memory.Scope(scope), false)
		if listErr != nil {
			return
		}
		count = len(entries)
	}
	if err := s.refreshMemoryRuntime(r, id, rec, count); err != nil {
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
	var count int
	if service, openErr := s.openAgentMemoryService(id, rec); openErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "memory_store_open_failed", openErr.Error(), nil)
		return
	} else {
		defer service.Close()
		// See the PATCH handler: UI memory management is allowed to target the
		// selected projection, while model tools remain scope-restricted.
		if _, err := service.Forget(r.Context(), memory.Scope(scope), entryID, "settings_delete"); err != nil {
			writeMemoryMutationError(w, err)
			return
		}
		entries, listErr := service.List(r.Context(), memory.Scope(scope), false)
		if listErr != nil {
			return
		}
		count = len(entries)
	}
	if err := s.refreshMemoryRuntime(r, id, rec, count); err != nil {
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
		return string(memory.ScopeAgent), nil
	}
	if scope != string(memory.ScopeAgent) && scope != string(memory.ScopeGlobal) {
		return "", errors.New("scope must be agent or global")
	}
	return scope, nil
}

func writeMemoryMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, memory.ErrNotFound) {
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
	pc, err := s.agents.EnsureAgentPromptContext(r.Context(), id)
	if err != nil {
		return err
	}
	if s.sessions != nil {
		content := promptContentFromRecord(pc)
		if content != nil {
			// Editing the non-active scope must not switch the agent's configured
			// memory scope. The edited scope is only the persistence target.
			runtimeScope := agentruntime.MemoryScopeFromDefaults(mustParseAgentSnapshot(rec))
			s.sessions.RefreshRuntimePromptContext(id, *content, runtimeScope)
		}
	}
	s.publishRuntimeConfigChanged(id, "prompt_context", true)
	s.publishMemoryChanged(id, "prompt_context", count, true)
	return nil
}

func (s *Server) writeMemoryMutationResponse(w http.ResponseWriter, r *http.Request, id string, rec *store.AgentRecord) {
	pc, err := s.agents.EnsureAgentPromptContext(r.Context(), id)
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
	scope := agentruntime.MemoryScopeFromDefaults(mustParseAgentSnapshot(agentRec))
	service, err := s.openAgentMemoryService(id, agentRec)
	if err != nil {
		return agentPromptContextView{}, err
	}
	defer service.Close()
	agentEntries, err := service.List(ctx, memory.ScopeAgent, false)
	if err != nil {
		return agentPromptContextView{}, err
	}
	globalEntries, err := service.List(ctx, memory.ScopeGlobal, false)
	if err != nil {
		return agentPromptContextView{}, err
	}
	return agentPromptContextView{
		AgentID:             id,
		SoulMD:              pc.SoulMD,
		CustomMD:            pc.CustomMD,
		MemoryScope:         scope,
		MemoryEntries:       memoryEntriesToViews(agentEntries),
		GlobalMemoryEntries: memoryEntriesToViews(globalEntries),
		Source:              "workspace_memory",
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

func normalizeMemoryScope(scope string) string {
	if strings.TrimSpace(scope) == string(memory.ScopeGlobal) {
		return string(memory.ScopeGlobal)
	}
	return string(memory.ScopeAgent)
}

func memoryEntriesToViews(entries []memory.Entry) []memoryEntryView {
	out := make([]memoryEntryView, 0, len(entries))
	for _, e := range entries {
		content := strings.TrimSpace(e.Content)
		if content == "" {
			continue
		}
		out = append(out, memoryEntryView{
			ID:        strings.TrimSpace(e.ID),
			Content:   content,
			CreatedAt: formatMemoryDate(e.CreatedAt),
			UpdatedAt: formatMemoryDate(e.UpdatedAt),
		})
	}
	return out
}

func memoryViewsToEntries(views []memoryEntryView, scope memory.Scope) []memory.Entry {
	now := time.Now().UTC()
	out := make([]memory.Entry, 0, len(views))
	for _, v := range views {
		content := strings.TrimSpace(v.Content)
		if content == "" {
			continue
		}
		id := strings.TrimSpace(v.ID)
		if id == "" {
			out = append(out, memory.Entry{ID: "", Scope: scope, Tier: memory.TierRecall, Kind: memory.KindFact, Content: content, Status: memory.StatusActive, SourceType: "settings", CreatedAt: now, UpdatedAt: now})
			continue
		}
		createdAt := parseMemoryDate(v.CreatedAt, now)
		updatedAt := parseMemoryDate(v.UpdatedAt, createdAt)
		out = append(out, memory.Entry{ID: id, Scope: scope, Tier: memory.TierRecall, Kind: memory.KindFact, Content: content, Status: memory.StatusActive, SourceType: "settings", CreatedAt: createdAt, UpdatedAt: updatedAt})
	}
	return out
}

func formatMemoryDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("20060102")
}

func parseMemoryDate(value string, fallback time.Time) time.Time {
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
	}
}
