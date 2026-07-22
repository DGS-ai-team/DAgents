package api

import (
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agenttemplate"
)

type agentTemplateView struct {
	agenttemplate.Template
	Source string `json:"source"` // builtin | user
}

func (s *Server) registerAgentTemplateRoutes() {
	s.mux.HandleFunc("GET /v1/agent-templates", s.handleListAgentTemplates)
	s.mux.HandleFunc("GET /v1/agent-templates/{id}", s.handleGetAgentTemplate)
	s.mux.HandleFunc("POST /v1/agent-templates", s.handleCreateAgentTemplate)
	s.mux.HandleFunc("DELETE /v1/agent-templates/{id}", s.handleDeleteAgentTemplate)
}

func (s *Server) templateViews() ([]agentTemplateView, error) {
	list, err := s.templateLoader().List()
	if err != nil {
		return nil, err
	}
	userDir := ""
	if s.cfg != nil {
		userDir = s.cfg.AgentTemplatesDir()
	}
	out := make([]agentTemplateView, 0, len(list))
	for _, t := range list {
		src := "builtin"
		if agenttemplate.IsUserTemplate(userDir, t.ID) {
			src = "user"
		}
		out = append(out, agentTemplateView{Template: t, Source: src})
	}
	return out, nil
}

func (s *Server) handleListAgentTemplates(w http.ResponseWriter, _ *http.Request) {
	views, err := s.templateViews()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "template_list_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": views})
}

func (s *Server) handleGetAgentTemplate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	t, err := s.templateLoader().Get(id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "template_not_found", err.Error(), nil)
		return
	}
	src := "builtin"
	if s.cfg != nil && agenttemplate.IsUserTemplate(s.cfg.AgentTemplatesDir(), id) {
		src = "user"
	}
	writeJSON(w, http.StatusOK, agentTemplateView{Template: t, Source: src})
}

func (s *Server) handleCreateAgentTemplate(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "config_unavailable", "node config not configured", nil)
		return
	}
	var body agenttemplate.Template
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	body.Normalize()
	if err := agenttemplate.ValidateID(body.ID); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_template_id", err.Error(), nil)
		return
	}
	if strings.TrimSpace(body.DisplayName) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_template", "display_name is required", nil)
		return
	}
	userDir := s.cfg.AgentTemplatesDir()
	if err := agenttemplate.SaveUser(userDir, body); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "template_save_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, agentTemplateView{
		Template: body,
		Source:   "user",
	})
}

func (s *Server) handleDeleteAgentTemplate(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "config_unavailable", "node config not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	userDir := s.cfg.AgentTemplatesDir()
	if !agenttemplate.IsUserTemplate(userDir, id) {
		writeAPIError(w, http.StatusForbidden, "template_not_deletable", "only user templates in runtime dir can be deleted", map[string]any{"id": id})
		return
	}
	if err := agenttemplate.DeleteUser(userDir, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "template_delete_failed", err.Error(), map[string]any{"id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}
