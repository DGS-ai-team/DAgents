package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
)

type mcpServerRequest struct {
	ID           string            `json:"id"`
	DisplayName  string            `json:"display_name"`
	Transport    string            `json:"transport"`
	Command      string            `json:"command"`
	Args         []string          `json:"args"`
	CWD          string            `json:"cwd"`
	URL          string            `json:"url"`
	EnvRefs      map[string]string `json:"env_refs"`
	HeaderRefs   map[string]string `json:"header_refs"`
	EnabledTools []string          `json:"enabled_tools"`
	Enabled      *bool             `json:"enabled"`
}

type mcpBindingsRequest struct {
	Bindings []mcp.Binding `json:"bindings"`
}

func (s *Server) registerMCPRoutes() {
	s.mux.HandleFunc("GET /v1/mcp/config", s.handleGetMCPConfig)
	s.mux.HandleFunc("PUT /v1/mcp/config", s.handlePutMCPConfig)
	s.mux.HandleFunc("GET /v1/mcp/servers", s.handleListMCPServers)
	s.mux.HandleFunc("POST /v1/mcp/servers", s.handleCreateMCPServer)
	s.mux.HandleFunc("PATCH /v1/mcp/servers/{server_id}", s.handlePatchMCPServer)
	s.mux.HandleFunc("DELETE /v1/mcp/servers/{server_id}", s.handleDeleteMCPServer)
	s.mux.HandleFunc("POST /v1/mcp/servers/{server_id}/test", s.handleTestMCPServer)
	s.mux.HandleFunc("POST /v1/mcp/servers/{server_id}/refresh", s.handleRefreshMCPServer)
	s.mux.HandleFunc("GET /v1/mcp/servers/{server_id}/tools", s.handleMCPServerTools)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/mcp", s.handleGetAgentMCP)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/mcp", s.handlePutAgentMCP)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/mcp/effective-tools", s.handleGetAgentEffectiveMCPTools)
}

func (s *Server) handleGetMCPConfig(w http.ResponseWriter, r *http.Request) {
	if s.mcpServers == nil || s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP store is not configured", nil)
		return
	}
	configs, err := s.mcpServers.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "mcp_config_load_failed", err.Error(), nil)
		return
	}
	configText, err := mcp.FormatConfigText(configs)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "mcp_config_format_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config_text": configText, "servers": s.mcpManager.List()})
}

func (s *Server) handlePutMCPConfig(w http.ResponseWriter, r *http.Request) {
	if s.mcpServers == nil || s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP store is not configured", nil)
		return
	}
	var body struct {
		ConfigText string `json:"config_text"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	existing, err := s.mcpServers.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "mcp_config_load_failed", err.Error(), nil)
		return
	}
	configs, err := mcp.ParseConfigText(body.ConfigText, existing)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_mcp_config", err.Error(), nil)
		return
	}
	if err := s.mcpServers.Replace(r.Context(), configs); err != nil {
		writeAPIError(w, http.StatusBadRequest, "mcp_config_save_failed", err.Error(), nil)
		return
	}
	if err := s.reloadMCPManager(r.Context()); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "mcp_manager_reload_failed", err.Error(), nil)
		return
	}
	for _, cfg := range configs {
		if _, refreshErr := s.mcpManager.Refresh(r.Context(), cfg.ID); refreshErr != nil {
			s.logger.Warn("refresh MCP server after config save failed", "server_id", cfg.ID, "error", refreshErr)
		}
	}
	s.reloadMCPBoundAgents(r.Context())
	configText, err := mcp.FormatConfigText(configs)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "mcp_config_format_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config_text": configText, "servers": s.mcpManager.List()})
}

func (s *Server) handleListMCPServers(w http.ResponseWriter, _ *http.Request) {
	if s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP manager is not configured", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": s.mcpManager.List()})
}

func (s *Server) handleCreateMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcpServers == nil || s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP store is not configured", nil)
		return
	}
	var req mcpServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	cfg, err := mcpConfigFromRequest(req, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_mcp_server", err.Error(), nil)
		return
	}
	if cfg, err = mcp.ValidateServerConfig(cfg); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_mcp_server", err.Error(), nil)
		return
	}
	if existing, _ := s.mcpServers.Get(r.Context(), cfg.ID); existing != nil {
		writeAPIError(w, http.StatusConflict, "mcp_server_exists", "MCP server already exists", nil)
		return
	}
	if err := s.mcpServers.Save(r.Context(), cfg); err != nil {
		writeAPIError(w, http.StatusBadRequest, "mcp_server_save_failed", err.Error(), nil)
		return
	}
	if err := s.reloadMCPManager(r.Context()); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "mcp_manager_reload_failed", err.Error(), nil)
		return
	}
	view, _ := s.mcpManager.Refresh(r.Context(), cfg.ID)
	s.reloadMCPBoundAgents(r.Context())
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handlePatchMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcpServers == nil || s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP store is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("server_id"))
	existing, err := s.mcpServers.Get(r.Context(), id)
	if err != nil || existing == nil {
		writeAPIError(w, http.StatusNotFound, "mcp_server_not_found", "MCP server not found", nil)
		return
	}
	var req mcpServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	cfg, err := mcpConfigFromRequest(req, existing.ID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_mcp_server", err.Error(), nil)
		return
	}
	if req.DisplayName == "" {
		cfg.DisplayName = existing.DisplayName
	}
	if req.Transport == "" && req.URL == "" {
		cfg.Transport = existing.Transport
	}
	if req.Command == "" {
		cfg.Command = existing.Command
	}
	if req.Args == nil {
		cfg.Args = existing.Args
	}
	if req.EnvRefs == nil {
		cfg.EnvRefs = existing.EnvRefs
	}
	if req.CWD == "" {
		cfg.CWD = existing.CWD
	}
	if req.URL == "" {
		cfg.URL = existing.URL
	}
	if req.HeaderRefs == nil {
		cfg.HeaderRefs = existing.HeaderRefs
	}
	if req.EnvRefs == nil {
		cfg.EnvValues = existing.EnvValues
	}
	if req.HeaderRefs == nil {
		cfg.HeaderValues = existing.HeaderValues
	}
	if req.EnabledTools == nil {
		cfg.EnabledTools = existing.EnabledTools
	}
	if req.Enabled == nil {
		cfg.Enabled = existing.Enabled
	}
	cfg, err = mcp.ValidateServerConfig(cfg)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_mcp_server", err.Error(), nil)
		return
	}
	if err := s.mcpServers.Save(r.Context(), cfg); err != nil {
		writeAPIError(w, http.StatusBadRequest, "mcp_server_save_failed", err.Error(), nil)
		return
	}
	if err := s.reloadMCPManager(r.Context()); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "mcp_manager_reload_failed", err.Error(), nil)
		return
	}
	view, _ := s.mcpManager.Get(cfg.ID)
	// Configure updates the cached enabled flags immediately. Only establish a
	// live connection when this server has no catalog yet; changing an
	// allowlist must not tear down an otherwise healthy MCP session.
	if cfg.Enabled && len(view.Tools) == 0 {
		view, _ = s.mcpManager.Refresh(r.Context(), cfg.ID)
	}
	s.reloadMCPBoundAgents(r.Context())
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcpServers == nil || s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP store is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("server_id"))
	if err := s.mcpServers.Delete(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusNotFound, "mcp_server_not_found", err.Error(), nil)
		return
	}
	if err := s.reloadMCPManager(r.Context()); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "mcp_manager_reload_failed", err.Error(), nil)
		return
	}
	s.reloadMCPBoundAgents(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleTestMCPServer(w http.ResponseWriter, r *http.Request) {
	s.handleMCPProbe(w, r, false)
}

func (s *Server) handleRefreshMCPServer(w http.ResponseWriter, r *http.Request) {
	s.handleMCPProbe(w, r, true)
}

func (s *Server) handleMCPProbe(w http.ResponseWriter, r *http.Request, persist bool) {
	if s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP manager is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("server_id"))
	var view mcp.ServerView
	var err error
	if persist {
		view, err = s.mcpManager.Refresh(r.Context(), id)
	} else {
		view, err = s.mcpManager.Test(r.Context(), id)
	}
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "mcp_probe_failed", err.Error(), map[string]any{"server": view})
		return
	}
	if persist {
		s.reloadMCPBoundAgents(r.Context())
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleMCPServerTools(w http.ResponseWriter, r *http.Request) {
	if s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP manager is not configured", nil)
		return
	}
	view, ok := s.mcpManager.Get(strings.TrimSpace(r.PathValue("server_id")))
	if !ok {
		writeAPIError(w, http.StatusNotFound, "mcp_server_not_found", "MCP server not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": view.Tools, "status": view.Status})
}

func (s *Server) handleGetAgentMCP(w http.ResponseWriter, r *http.Request) {
	id, rec, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_invalid", err.Error(), nil)
		return
	}
	bindings := mcp.BindingsFromDefaults(snap.Defaults)
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": id, "bindings": bindings})
}

func (s *Server) handlePutAgentMCP(w http.ResponseWriter, r *http.Request) {
	id, rec, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	var req mcpBindingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP manager is not configured", nil)
		return
	}
	if err := s.mcpManager.ValidateBindings(req.Bindings); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_mcp_binding", err.Error(), nil)
		return
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_invalid", err.Error(), nil)
		return
	}
	snap.Defaults = mcp.BindingsToDefaults(snap.Defaults, req.Bindings)
	raw, err := json.Marshal(map[string]any{"template_id": snap.TemplateID, "defaults": snap.Defaults})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_encode_failed", err.Error(), nil)
		return
	}
	rec.ConfigSnapshot = raw
	if err := s.agents.Save(r.Context(), *rec); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_save_failed", err.Error(), nil)
		return
	}
	updated, err := s.agents.Get(r.Context(), id)
	if err != nil || updated == nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_reload_record_failed", "agent record unavailable after save", nil)
		return
	}
	applied, err := s.reloadAgentRuntimeIfIdle(r.Context(), *updated, "mcp_binding")
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "agent_mcp_reload_failed", err.Error(), nil)
		return
	}
	s.publishRuntimeConfigChanged(id, "mcp_binding", applied)
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": id, "bindings": mcp.BindingsFromDefaults(snap.Defaults), "runtime_applied": applied})
}

func (s *Server) handleGetAgentEffectiveMCPTools(w http.ResponseWriter, r *http.Request) {
	_, rec, ok := s.requireAgentRecord(w, r)
	if !ok {
		return
	}
	if s.mcpManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "mcp_unavailable", "MCP manager is not configured", nil)
		return
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_invalid", err.Error(), nil)
		return
	}
	effective, err := s.mcpManager.EffectiveTools(r.Context(), mcp.BindingsFromDefaults(snap.Defaults))
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "mcp_tools_unavailable", err.Error(), nil)
		return
	}
	tools := make([]map[string]any, 0, len(effective))
	for _, tool := range effective {
		tools = append(tools, map[string]any{
			"name": tool.QualifiedName, "server_id": tool.ServerID, "remote_name": tool.RemoteName,
			"description": tool.Description, "input_schema": tool.InputSchema,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (s *Server) reloadMCPManager(ctx context.Context) error {
	if s.mcpServers == nil || s.mcpManager == nil {
		return nil
	}
	configs, err := s.mcpServers.List(ctx)
	if err != nil {
		return err
	}
	return s.mcpManager.Configure(configs)
}

func (s *Server) reloadMCPBoundAgents(ctx context.Context) {
	if s == nil || s.agents == nil || s.mcpManager == nil {
		return
	}
	records, err := s.agents.List(ctx)
	if err != nil {
		s.logger.Warn("list agents for MCP reload failed", "error", err)
		return
	}
	for _, rec := range records {
		snap, parseErr := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
		if parseErr != nil {
			continue
		}
		bindings := mcp.BindingsFromDefaults(snap.Defaults)
		if len(bindings) == 0 {
			continue
		}
		applied, err := s.reloadAgentRuntimeIfIdle(ctx, rec, "mcp_catalog")
		if err != nil {
			s.logger.Warn("reload agent after MCP catalog change failed", "agent_id", rec.AgentID, "error", err)
			continue
		}
		s.publishRuntimeConfigChanged(rec.AgentID, "mcp_catalog", applied)
		if s.stream != nil {
			s.stream.Publish(rec.AgentID, "mcp/catalog-changed", map[string]any{
				"agent_id": rec.AgentID,
				"applied":  applied,
			})
		}
	}
}

func mcpConfigFromRequest(req mcpServerRequest, fallbackID string) (mcp.ServerConfig, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = strings.TrimSpace(fallbackID)
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return mcp.ServerConfig{
		ID: id, DisplayName: strings.TrimSpace(req.DisplayName), Transport: strings.TrimSpace(req.Transport),
		Command: strings.TrimSpace(req.Command), Args: req.Args, CWD: strings.TrimSpace(req.CWD), URL: strings.TrimSpace(req.URL),
		EnvRefs: req.EnvRefs, HeaderRefs: req.HeaderRefs, Enabled: enabled,
		EnabledTools: req.EnabledTools,
	}, nil
}
