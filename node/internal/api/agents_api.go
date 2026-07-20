package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	Sandbox     *struct {
		Enabled *bool   `json:"enabled"`
		Backend *string `json:"backend"`
	} `json:"sandbox"`
}

type agentView struct {
	AgentID        string          `json:"agent_id"`
	DisplayName    string          `json:"display_name"`
	TemplateID     string          `json:"template_id"`
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
		"defaults":    tpl.Defaults,
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
	// 过渡：同步创建同 id 的内部 session，使现有 messages/hydrate 路径可用（Phase 2 再切纯 agent）。
	if s.sessions != nil {
		if _, _, err := s.sessions.Create(agentID); err != nil {
			s.logger.Warn("bridge session create failed", "agent_id", agentID, "error", err)
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
	if req.DisplayName == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_patch", "display_name is required", nil)
		return
	}
	name := strings.TrimSpace(*req.DisplayName)
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_patch", "display_name cannot be empty", nil)
		return
	}
	rec.DisplayName = name
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
