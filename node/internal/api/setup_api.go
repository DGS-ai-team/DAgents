package api

import (
	"net/http"
	"os"

	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func (s *Server) registerSetupRoutes() {
	s.mux.HandleFunc("GET /v1/setup/config", s.handleGetSetupConfig)
	s.mux.HandleFunc("PATCH /v1/setup/config", s.handlePatchSetupConfig)
}

func (s *Server) handleGetSetupConfig(w http.ResponseWriter, _ *http.Request) {
	view := setup.ViewFromConfig(s.cfg)
	view.ConfigPath = s.configPath
	view.ConfigWritable = configPathWritable(s.configPath)
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handlePatchSetupConfig(w http.ResponseWriter, r *http.Request) {
	if s.configPath == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "config_path_unknown", "Node 未记录 config.yaml 路径，无法保存", nil)
		return
	}
	if !configPathWritable(s.configPath) {
		writeAPIError(w, http.StatusForbidden, "config_not_writable", "config.yaml 不可写", nil)
		return
	}
	var patch setup.SettingsPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if !setup.PatchHasBlock(patch) {
		writeAPIError(w, http.StatusBadRequest, "invalid_patch", "至少提供一个配置块", nil)
		return
	}
	updated, err := setup.ApplyPatch(s.cfg, patch)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_setup_config", err.Error(), nil)
		return
	}
	if err := config.SaveFile(s.configPath, updated); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "config_save_failed", err.Error(), nil)
		return
	}
	setup.CopyConfig(s.cfg, updated)
	if s.llmRuntime != nil {
		s.llmRuntime.SyncFromConfig(s.cfg)
	}
	view := setup.ViewFromConfig(s.cfg)
	view.ConfigPath = s.configPath
	view.ConfigWritable = true
	view.RestartRequired = true
	writeJSON(w, http.StatusOK, view)
}

func configPathWritable(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	if err == nil {
		file, openErr := os.OpenFile(path, os.O_WRONLY, 0)
		if openErr != nil {
			return false
		}
		_ = file.Close()
		return true
	}
	if !os.IsNotExist(err) {
		return false
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return false
	}
	_ = file.Close()
	_ = os.Remove(path)
	return true
}
