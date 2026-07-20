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
	"github.com/DGS-ai-team/DAgents/node/internal/store"
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
	TemplateID  string `json:"template_id"`
	DisplayName string `json:"display_name"`
	Origin      string `json:"origin"` // 预留：local | remote；缺省 local
	Sandbox     *struct {
		Enabled *bool   `json:"enabled"`
		Backend *string `json:"backend"`
	} `json:"sandbox"`
	Defaults map[string]any `json:"defaults"`
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
	if tplID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "template_id is required", nil)
		return
	}
	tpl, err := s.templateLoader().Get(tplID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "template_not_found", err.Error(), nil)
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		name = strings.TrimSpace(tpl.DisplayName)
	}
	if name == "" {
		name = tpl.ID
	}
	sandboxEnabled := tpl.Sandbox.Enabled
	sandboxBackend := strings.TrimSpace(tpl.Sandbox.Backend)
	if sandboxBackend == "" {
		sandboxBackend = "process"
	}
	if req.Sandbox != nil {
		if req.Sandbox.Enabled != nil {
			sandboxEnabled = *req.Sandbox.Enabled
		}
		if req.Sandbox.Backend != nil && strings.TrimSpace(*req.Sandbox.Backend) != "" {
			sandboxBackend = strings.TrimSpace(*req.Sandbox.Backend)
		}
	}
	switch strings.ToLower(sandboxBackend) {
	case "process", "docker":
		sandboxBackend = strings.ToLower(sandboxBackend)
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_sandbox", "sandbox.backend must be process|docker", nil)
		return
	}
	if sandboxBackend == "docker" {
		// Phase 1：仅持久化配置；Docker 执行器后续实现。
		s.logger.Info("agent sandbox backend=docker reserved for later implementation")
	}

	agentID, err := generateAgentInstanceID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_id_failed", err.Error(), nil)
		return
	}

	snapshot := map[string]any{
		"template_id": tpl.ID,
		"defaults":    agentruntime.MergeDefaults(tpl.Defaults, req.Defaults),
		"sandbox": map[string]any{
			"enabled":              sandboxEnabled,
			"backend":              sandboxBackend,
			"workspace_subdir":     tpl.Sandbox.WorkspaceSubdir,
			"fs_root_isolation":    tpl.Sandbox.FSRootIsolation,
			"allow_bash":           tpl.Sandbox.AllowBash,
			"allow_network_tools":  tpl.Sandbox.AllowNetworkTools,
			"image":                tpl.Sandbox.Image,
			"network":              tpl.Sandbox.Network,
			"memory":               tpl.Sandbox.Memory,
			"cpus":                 tpl.Sandbox.CPUs,
		},
	}
	snapRaw, _ := json.Marshal(snapshot)
	now := time.Now().UTC()
	rec := store.AgentRecord{
		AgentID:        agentID,
		DisplayName:    name,
		TemplateID:     tpl.ID,
		Origin:         store.NormalizeAgentOrigin(req.Origin),
		SandboxEnabled: sandboxEnabled,
		SandboxBackend: sandboxBackend,
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
	// Phase 2：按快照构造 per-agent FSRoot / Registry，并桥接同 id 的内部 session。
	if s.sessions != nil {
		snapParsed, err := agentruntime.ParseSnapshot(snapRaw)
		if err != nil {
			s.logger.Warn("parse agent snapshot failed", "agent_id", agentID, "error", err)
		} else {
			built, err := agentruntime.Build(agentruntime.BuildParams{
				NodeCFG:   s.cfg,
				BaseTurn:  s.sessions.DefaultTurnOptions(),
				AgentID:   agentID,
				Snapshot:  snapParsed,
			})
			if err != nil {
				writeAPIError(w, http.StatusInternalServerError, "agent_runtime_failed", err.Error(), nil)
				return
			}
			if _, _, err := s.sessions.CreateWithOptions(agentID, built.TurnOptions, built.Registry); err != nil {
				s.logger.Warn("bridge session create failed", "agent_id", agentID, "error", err)
			} else {
				s.logger.Info("agent runtime ready", "agent_id", agentID, "fs_root", built.FSRoot, "tool_groups", built.ToolGroups)
			}
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
		views = append(views, agentViewFromRecord(rec))
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
	writeJSON(w, http.StatusOK, agentViewFromRecord(*rec))
}

type patchAgentRequest struct {
	DisplayName *string `json:"display_name"`
	LLMActive   *string `json:"llm_active"`
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
	if req.DisplayName == nil && req.LLMActive == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_patch", "display_name or llm_active is required", nil)
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
		snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_invalid", err.Error(), nil)
			return
		}
		if snap.Defaults == nil {
			snap.Defaults = map[string]any{}
		}
		llmMap, _ := snap.Defaults["llm"].(map[string]any)
		if llmMap == nil {
			llmMap = map[string]any{}
		}
		llmMap["active"] = active
		snap.Defaults["llm"] = llmMap
		raw, err := json.Marshal(snap)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_encode_failed", err.Error(), nil)
			return
		}
		rec.ConfigSnapshot = raw
		if err := s.switchActiveLLMProfile(active); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_llm_settings", err.Error(), nil)
			return
		}
	}
	rec.UpdatedAt = time.Now().UTC()
	if err := s.agents.Save(r.Context(), *rec); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_save_failed", err.Error(), nil)
		return
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
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id})
}

func (s *Server) ensureAgentWorkspace(agentID string) error {
	if s.cfg == nil {
		return nil
	}
	root := filepath.Join(s.cfg.AgentsDir(), agentID)
	for _, sub := range []string{"data", "policy", "history", "memory"} {
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
// Node 重启或 idle Release 后必须调用，否则会落到默认 TurnOptions / Registry。
func (s *Server) ensureAgentRuntime(ctx context.Context, agentID string) error {
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
	snapParsed, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return fmt.Errorf("parse agent snapshot: %w", err)
	}
	// 切换 / 确保 Agent 时应用其绑定的 LLM 配置。
	if active := agentruntime.LLMActiveFromDefaults(snapParsed); active != "" {
		if err := s.switchActiveLLMProfile(active); err != nil {
			s.logger.Warn("apply agent llm profile failed", "agent_id", id, "profile", active, "error", err)
		}
	}
	if s.sessions.Get(id) != nil {
		return nil
	}
	if err := s.ensureAgentWorkspace(id); err != nil {
		s.logger.Warn("agent workspace ensure failed", "agent_id", id, "error", err)
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
	if _, _, err := s.sessions.CreateWithOptions(id, built.TurnOptions, built.Registry); err != nil {
		return err
	}
	s.logger.Info("agent runtime ensured", "agent_id", id, "fs_root", built.FSRoot, "tool_groups", built.ToolGroups)
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
