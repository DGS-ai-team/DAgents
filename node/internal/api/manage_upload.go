package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

type manageUploadSkillBody struct {
	Path     string `json:"path"`
	SkillID  string `json:"skill_id"`
	Version  string `json:"version"`
	Name     string `json:"name"`
	Publish  bool   `json:"publish"`
}

type manageUploadExternalToolBody struct {
	Path     string `json:"path"`
	ToolID   string `json:"tool_id"`
	Version  string `json:"version"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Publish  bool   `json:"publish"`
}

type manageUploadPluginBody struct {
	Path     string `json:"path"`
	PluginID string `json:"plugin_id"`
	Version  string `json:"version"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Publish  bool   `json:"publish"`
}

func (s *Server) registerManageUploadRoutes() {
	if s.packageUploader == nil || !s.packageUploader.Enabled() {
		return
	}
	s.mux.HandleFunc("POST /v1/manage/upload/skill", s.handleManageUploadSkill)
	s.mux.HandleFunc("POST /v1/manage/upload/externaltool", s.handleManageUploadExternalTool)
	s.mux.HandleFunc("POST /v1/manage/upload/plugin", s.handleManageUploadPlugin)
}

func (s *Server) handleManageUploadSkill(w http.ResponseWriter, r *http.Request) {
	if s.packageUploader == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "manage_disabled", "manage upload disabled", nil)
		return
	}
	var body manageUploadSkillBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = body.SkillID
	}
	out, err := s.packageUploader.UploadSkill(r.Context(), body.Path, body.SkillID, body.Version, name, body.Publish)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_upload_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageUploadExternalTool(w http.ResponseWriter, r *http.Request) {
	if s.packageUploader == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "manage_disabled", "manage upload disabled", nil)
		return
	}
	var body manageUploadExternalToolBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = body.ToolID
	}
	out, err := s.packageUploader.UploadExternalTool(
		r.Context(), body.Path, body.ToolID, body.Version, name, body.Platform, body.Publish,
	)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_upload_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManageUploadPlugin(w http.ResponseWriter, r *http.Request) {
	if s.packageUploader == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "manage_disabled", "manage upload disabled", nil)
		return
	}
	var body manageUploadPluginBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = body.PluginID
	}
	out, err := s.packageUploader.UploadPlugin(
		r.Context(), body.Path, body.PluginID, body.Version, name, body.Platform, body.Publish,
	)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "manage_upload_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
