package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/agenttemplate"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/sandbox"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func (s *Server) registerAgentRoutes() {
	s.mux.HandleFunc("GET /v1/agent-templates", s.handleListAgentTemplates)
	s.mux.HandleFunc("GET /v1/agent-templates/{id}", s.handleGetAgentTemplate)
	s.mux.HandleFunc("POST /v1/agents", s.handleCreateAgent)
	s.mux.HandleFunc("GET /v1/agents", s.handleListAgents)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}", s.handleGetAgent)
	s.mux.HandleFunc("PATCH /v1/agents/{agent_id}", s.handlePatchAgent)
	s.mux.HandleFunc("DELETE /v1/agents/{agent_id}", s.handleDeleteAgent)
	// Phase 2–4：agent 路径别名（内部仍走 session 实现，id 相同）。
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/ensure", s.handleAgentEnsure)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/reload", s.handleAgentReload)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/hydrate", s.handleAgentHydrate)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/cancel", s.handleAgentCancel)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/context", s.handleAgentContext)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/ack", s.handleAgentAck)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/clear-context", s.handleAgentClearContext)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/compress", s.handleAgentCompress)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/skills", s.handleAgentListSkills)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/skills/load", s.handleAgentLoadSkill)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/skills/unload", s.handleAgentUnloadSkill)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/child-agents", s.handleAgentListChildAgents)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/child-agents/{child_session_id}", s.handleAgentGetChildAgent)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/child-agents/{child_session_id}/cancel", s.handleAgentCancelChildAgent)
	s.registerAgentPolicyRoutes()
}

func (s *Server) templateLoader() *agenttemplate.Loader {
	builtin := agenttemplate.ResolveBuiltinDir()
	userDir := ""
	if s.cfg != nil {
		userDir = s.cfg.AgentTemplatesDir()
	}
	return agenttemplate.NewLoader(builtin, userDir)
}

func (s *Server) handleListAgentTemplates(w http.ResponseWriter, _ *http.Request) {
	list, err := s.templateLoader().List()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "template_list_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": list})
}

func (s *Server) handleGetAgentTemplate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	t, err := s.templateLoader().Get(id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "template_not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type createAgentRequest struct {
	// TemplateID 仅作溯源（可选）；配置由前端展开后通过 defaults/sandbox 完整提交。
	TemplateID  string         `json:"template_id"`
	DisplayName string         `json:"display_name"`
	Origin      string         `json:"origin"` // 预留：local | remote；缺省 local
	Sandbox     *sandboxPatch  `json:"sandbox"`
	Defaults    map[string]any `json:"defaults"`
}

type agentView struct {
	AgentID        string          `json:"agent_id"`
	DisplayName    string          `json:"display_name"`
	TemplateID     string          `json:"template_id"`
	Origin         string          `json:"origin"`
	SandboxEnabled bool            `json:"sandbox_enabled"`
	SandboxBackend string          `json:"sandbox_backend"`
	ConfigSnapshot json.RawMessage `json:"config_snapshot,omitempty"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	// 以下字段供托盘 / 通知同步（agent_id 与内部 session 1:1）。
	Active           bool   `json:"active,omitempty"`
	HasActiveTurn    bool   `json:"has_active_turn,omitempty"`
	RunTurnPhase     string `json:"run_turn_phase,omitempty"`
	NotifySeq        int    `json:"notify_seq,omitempty"`
	AckSeq           int    `json:"ack_seq,omitempty"`
	HasUnread        bool   `json:"has_unread,omitempty"`
	HasPendingHITL   bool   `json:"has_pending_hitl,omitempty"`
	PendingHITLItems int    `json:"pending_hitl_items,omitempty"`
}

func agentViewFromRecord(rec store.AgentRecord) agentView {
	return agentView{
		AgentID:        rec.AgentID,
		DisplayName:    rec.DisplayName,
		TemplateID:     rec.TemplateID,
		Origin:         store.NormalizeAgentOrigin(rec.Origin),
		SandboxEnabled: rec.SandboxEnabled,
		SandboxBackend: rec.SandboxBackend,
		ConfigSnapshot: rec.ConfigSnapshot,
		CreatedAt:      rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	var req createAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}

	tplID := strings.TrimSpace(req.TemplateID)
	var tpl *agenttemplate.Template
	// 无完整 defaults 时才需要加载模板做种子；有 defaults 时 template_id 仅溯源，不强制模板存在。
	if tplID != "" && req.Defaults == nil {
		loaded, err := s.templateLoader().Get(tplID)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "template_not_found", err.Error(), nil)
			return
		}
		tpl = &loaded
	} else if tplID != "" {
		if loaded, err := s.templateLoader().Get(tplID); err == nil {
			tpl = &loaded
		}
	}

	// 完整设置：前端展开模板后提交 defaults；无 defaults 时用模板种子（兼容旧客户端）。
	fullSettings := req.Defaults != nil
	if !fullSettings && tpl == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "defaults or template_id is required", nil)
		return
	}

	name := strings.TrimSpace(req.DisplayName)
	if name == "" && tpl != nil {
		name = strings.TrimSpace(tpl.DisplayName)
		if name == "" {
			name = tpl.ID
		}
	}
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "display_name is required", nil)
		return
	}

	baseSandbox := agentruntime.SandboxSpec{
		Backend: "process", WorkspaceSubdir: "data", AllowBash: true, AllowNetworkTools: true,
	}
	baseDefaults := map[string]any{}
	if fullSettings {
		// 完整入参：不再服务端合并模板；template_id 仅溯源。
		if req.Defaults != nil {
			baseDefaults = agentruntime.MergeDefaults(nil, req.Defaults)
		}
		if req.Sandbox == nil {
			// 允许只传 defaults，沙箱用安全默认值。
		}
	} else if tpl != nil {
		baseSandbox = sandboxFromTemplate(tpl)
		baseDefaults = agentruntime.MergeDefaults(tpl.Defaults, nil)
	}

	sandbox, err := applySandboxPatch(baseSandbox, req.Sandbox)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_sandbox", err.Error(), nil)
		return
	}
	if err := requireDockerSandboxReady(sandbox); err != nil {
		writeAPIError(w, http.StatusBadRequest, "docker_unavailable", err.Error(), nil)
		return
	}

	agentID, err := generateAgentInstanceID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_id_failed", err.Error(), nil)
		return
	}

	snapRaw, err := marshalAgentSnapshot(tplID, baseDefaults, sandbox)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_encode_failed", err.Error(), nil)
		return
	}
	now := time.Now().UTC()
	rec := store.AgentRecord{
		AgentID:        agentID,
		DisplayName:    name,
		TemplateID:     tplID,
		Origin:         store.NormalizeAgentOrigin(req.Origin),
		SandboxEnabled: sandbox.Enabled,
		SandboxBackend: sandbox.Backend,
		ConfigSnapshot: snapRaw,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.agents.Save(r.Context(), rec); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_save_failed", err.Error(), nil)
		return
	}
	if err := s.ensureAgentWorkspace(agentID); err != nil {
		s.logger.Warn("agent workspace create failed", "agent_id", agentID, "error", err)
	}
	if _, err := s.agents.EnsureAgentPolicy(r.Context(), agentID, s.runtimeDir()); err != nil {
		s.logger.Warn("agent policy seed failed", "agent_id", agentID, "error", err)
	}
	if _, err := s.agents.EnsureAgentPromptContext(r.Context(), agentID, s.runtimeDir()); err != nil {
		s.logger.Warn("agent prompt context seed failed", "agent_id", agentID, "error", err)
	}
	if s.sessions != nil {
		if err := s.reloadAgentRuntime(r.Context(), rec); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_runtime_failed", err.Error(), nil)
			return
		}
	}
	writeJSON(w, http.StatusOK, agentViewFromRecord(rec))
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	list, err := s.agents.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_list_failed", err.Error(), nil)
		return
	}
	views := make([]agentView, 0, len(list))
	for _, rec := range list {
		views = append(views, s.enrichAgentNotify(agentViewFromRecord(rec)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": views})
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("agent_id"))
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
		return
	}
	if rec == nil || rec.Archived {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
		return
	}
	writeJSON(w, http.StatusOK, s.enrichAgentNotify(agentViewFromRecord(*rec)))
}

// enrichAgentNotify 附加未读 / HITL 等通知字段（托盘待办同步用）。
func (s *Server) enrichAgentNotify(v agentView) agentView {
	if s.sessions == nil || strings.TrimSpace(v.AgentID) == "" {
		return v
	}
	notify := s.sessions.NotificationState(v.AgentID)
	v.NotifySeq = notify.NotifySeq
	v.AckSeq = notify.AckSeq
	v.HasUnread = notify.HasUnread
	v.HasPendingHITL = notify.HasPendingHITL
	v.PendingHITLItems = notify.PendingHITLItems
	if s.sessions.Get(v.AgentID) != nil {
		v.Active = true
		_, hasActiveTurn, turnState, _ := s.sessions.RuntimeInfo(v.AgentID)
		v.HasActiveTurn = hasActiveTurn
		v.RunTurnPhase = turn.RunTurnPhase(turnState)
	} else if notify.HasPendingHITL {
		v.RunTurnPhase = "waiting_hitl"
	}
	return v
}

type patchAgentRequest struct {
	DisplayName *string        `json:"display_name"`
	LLMActive   *string        `json:"llm_active"` // 兼容快捷字段；等价于 defaults.llm.active
	Sandbox     *sandboxPatch  `json:"sandbox"`
	Defaults    map[string]any `json:"defaults"` // 深合并进快照
}

func (s *Server) handlePatchAgent(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("agent_id"))
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
		return
	}
	if rec == nil || rec.Archived {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
		return
	}
	var req patchAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if req.DisplayName == nil && req.LLMActive == nil && req.Sandbox == nil && req.Defaults == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_patch", "no patch fields", nil)
		return
	}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if name == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_patch", "display_name cannot be empty", nil)
			return
		}
		rec.DisplayName = name
	}

	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_invalid", err.Error(), nil)
		return
	}
	if snap.Defaults == nil {
		snap.Defaults = map[string]any{}
	}
	runtimeDirty := false

	if req.Defaults != nil {
		snap.Defaults = agentruntime.MergeDefaults(snap.Defaults, req.Defaults)
		runtimeDirty = true
	}
	if req.LLMActive != nil {
		active := strings.TrimSpace(*req.LLMActive)
		if active == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_patch", "llm_active cannot be empty", nil)
			return
		}
		if s.cfg != nil {
			if _, ok := s.cfg.LLM.GetProfile(active); !ok {
				writeAPIError(w, http.StatusBadRequest, "invalid_patch", fmt.Sprintf("llm profile %q not found", active), nil)
				return
			}
		}
		llmMap, _ := snap.Defaults["llm"].(map[string]any)
		if llmMap == nil {
			llmMap = map[string]any{}
		}
		llmMap["active"] = active
		snap.Defaults["llm"] = llmMap
		runtimeDirty = true
	}
	if req.Sandbox != nil {
		sandbox, err := applySandboxPatch(snap.Sandbox, req.Sandbox)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_sandbox", err.Error(), nil)
			return
		}
		if err := requireDockerSandboxReady(sandbox); err != nil {
			writeAPIError(w, http.StatusBadRequest, "docker_unavailable", err.Error(), nil)
			return
		}
		snap.Sandbox = sandbox
		rec.SandboxEnabled = sandbox.Enabled
		rec.SandboxBackend = sandbox.Backend
		runtimeDirty = true
	}

	if runtimeDirty {
		raw, err := marshalAgentSnapshot(snap.TemplateID, snap.Defaults, snap.Sandbox)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_encode_failed", err.Error(), nil)
			return
		}
		rec.ConfigSnapshot = raw
	}
	rec.UpdatedAt = time.Now().UTC()
	if err := s.agents.Save(r.Context(), *rec); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_save_failed", err.Error(), nil)
		return
	}
	if runtimeDirty && s.sessions != nil {
		if err := s.reloadAgentRuntime(r.Context(), *rec); err != nil {
			s.logger.Warn("agent runtime reload after patch failed", "agent_id", id, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, agentViewFromRecord(*rec))
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if err := s.agents.SoftDelete(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", err.Error(), map[string]any{"agent_id": id})
		return
	}
	if s.sessions != nil {
		_, _ = s.sessions.Delete(id)
	} else if s.sandboxPool != nil {
		s.sandboxPool.Release(id)
	} else {
		sandbox.ReleaseAgent(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id})
}

func (s *Server) ensureAgentWorkspace(agentID string) error {
	if s.cfg == nil {
		return nil
	}
	root := filepath.Join(s.cfg.AgentsDir(), agentID)
	for _, sub := range []string{"data", "history", "memory"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func generateAgentInstanceID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("agt-%x", b), nil
}

// ensureAgentRuntime 按 agents.db 快照把 Agent 装入内存（CreateWithOptions）。
// 若内存已有但配置版本落后（UpdatedAt 变更），会自动 Release 后重建。
func (s *Server) ensureAgentRuntime(ctx context.Context, agentID string) error {
	return s.ensureAgentRuntimeOpts(ctx, agentID, false)
}

func (s *Server) ensureAgentRuntimeOpts(ctx context.Context, agentID string, forceReload bool) error {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	if s.agents == nil {
		return fmt.Errorf("agents store not configured")
	}
	if s.sessions == nil {
		return fmt.Errorf("sessions manager not configured")
	}
	rec, err := s.agents.Get(ctx, id)
	if err != nil {
		return err
	}
	if rec == nil || rec.Archived {
		return fmt.Errorf("agent_not_found")
	}
	rev := rec.UpdatedAt.UTC().UnixNano()
	if !forceReload && s.sessions.Get(id) != nil {
		if s.sessions.ConfigRevision(id) == rev {
			return nil
		}
		forceReload = true
	}
	if forceReload {
		_, _ = s.sessions.Release(id)
	}
	return s.reloadAgentRuntime(ctx, *rec)
}

func (s *Server) reloadAgentRuntime(ctx context.Context, rec store.AgentRecord) error {
	id := strings.TrimSpace(rec.AgentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	if s.sessions == nil {
		return fmt.Errorf("sessions manager not configured")
	}
	snapParsed, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return fmt.Errorf("parse agent snapshot: %w", err)
	}
	if err := requireDockerSandboxReady(snapParsed.Sandbox); err != nil {
		return err
	}
	if err := s.ensureAgentWorkspace(id); err != nil {
		s.logger.Warn("agent workspace ensure failed", "agent_id", id, "error", err)
	}
	var policyEngine *policy.Engine
	if s.agents != nil {
		engine, err := s.agents.LoadAgentPolicyEngine(ctx, id, s.runtimeDir())
		if err != nil {
			s.logger.Warn("agent policy load failed", "agent_id", id, "error", err)
		} else {
			policyEngine = engine
		}
	}
	if s.sessions.Get(id) != nil {
		_, _ = s.sessions.Release(id)
	}
	built, err := agentruntime.Build(agentruntime.BuildParams{
		NodeCFG:  s.cfg,
		BaseTurn: s.sessions.DefaultTurnOptions(),
		AgentID:  id,
		Snapshot: snapParsed,
	})
	if err != nil {
		return fmt.Errorf("build agent runtime: %w", err)
	}
	attachTriggerRuntime(built.Registry, s.triggerStore, s.triggerSched, id)
	built.TurnOptions.ConfigRevision = rec.UpdatedAt.UTC().UnixNano()
	if s.agents != nil {
		if pc, err := s.agents.EnsureAgentPromptContext(ctx, id, s.runtimeDir()); err != nil {
			s.logger.Warn("agent prompt context load failed", "agent_id", id, "error", err)
		} else {
			built.TurnOptions.PromptContent = promptContentFromRecord(pc)
		}
	}
	if _, _, err := s.sessions.CreateWithOptions(id, built.TurnOptions, built.Registry, policyEngine); err != nil {
		return err
	}
	if runner := built.Registry.DockerSandbox(); runner != nil {
		pool := s.sandboxPool
		if pool == nil {
			pool = sandbox.NewPool(sandbox.DefaultIdleTimeout, s.logger)
			s.sandboxPool = pool
		}
		if err := pool.Ensure(ctx, runner); err != nil {
			_, _ = s.sessions.Release(id)
			return fmt.Errorf("docker sandbox ensure: %w", err)
		}
		s.logger.Info("docker sandbox ready", "agent_id", id, "container", runner.Name, "image", runner.Spec.Image)
	}
	s.logger.Info("agent runtime ready", "agent_id", id, "fs_root", built.FSRoot, "tool_groups", built.ToolGroups)
	return nil
}

func (s *Server) handleAgentEnsure(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if err := s.ensureAgentRuntime(r.Context(), id); err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", err.Error(), map[string]any{"agent_id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id})
}

func (s *Server) handleAgentReload(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if err := s.ensureAgentRuntimeOpts(r.Context(), id, true); err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "agent_reload_failed", err.Error(), map[string]any{"agent_id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id, "reloaded": true})
}

// agent 路径别名：把 PathValue agent_id 映射为 session_id 后复用既有 handler。
func (s *Server) withAgentAsSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("agent_id"))
		if id == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
			return
		}
		if err := s.ensureAgentRuntime(r.Context(), id); err != nil {
			if err.Error() == "agent_not_found" {
				writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", err.Error(), map[string]any{"agent_id": id})
			return
		}
		r.SetPathValue("session_id", id)
		next(w, r)
	}
}

func (s *Server) handleAgentHydrate(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleSessionHydrate)(w, r)
}

func (s *Server) handleAgentCancel(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleCancelSession)(w, r)
}

func (s *Server) handleAgentContext(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleSessionContext)(w, r)
}

func (s *Server) handleAgentAck(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleSessionAck)(w, r)
}

func (s *Server) handleAgentClearContext(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleClearContext)(w, r)
}

func (s *Server) handleAgentCompress(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleCompressContext)(w, r)
}

func (s *Server) handleAgentListSkills(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleListSessionSkills)(w, r)
}

func (s *Server) handleAgentLoadSkill(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleLoadSessionSkill)(w, r)
}

func (s *Server) handleAgentUnloadSkill(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleUnloadSessionSkill)(w, r)
}

func (s *Server) handleAgentListChildAgents(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleListChildAgents)(w, r)
}

func (s *Server) handleAgentGetChildAgent(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleGetChildAgent)(w, r)
}

func (s *Server) handleAgentCancelChildAgent(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleCancelChildAgent)(w, r)
}
