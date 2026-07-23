package api

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func (s *Server) registerSetupRoutes() {
	s.mux.HandleFunc("GET /v1/setup/config", s.handleGetSetupConfig)
	s.mux.HandleFunc("PATCH /v1/setup/config", s.handlePatchSetupConfig)
}

func (s *Server) handleGetSetupConfig(w http.ResponseWriter, _ *http.Request) {
	view := setup.ViewFromConfig(s.cfg)
	s.enrichLLMSettingsView(&view.LLM)
	view.ConfigPath = s.configPath
	view.ConfigWritable = configPathWritable(s.configPath) || s.llmConfigs != nil
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handlePatchSetupConfig(w http.ResponseWriter, r *http.Request) {
	yamlWritable := s.configPath != "" && configPathWritable(s.configPath)
	if !yamlWritable && s.llmConfigs == nil {
		if s.configPath == "" {
			writeAPIError(w, http.StatusServiceUnavailable, "config_path_unknown", "Node 未记录 config.yaml 路径，无法保存", nil)
			return
		}
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

	// 非 LLM 块仍需可写 yaml。
	if patch.LLM == nil || setup.PatchHasNonLLMBlock(patch) {
		if !yamlWritable {
			writeAPIError(w, http.StatusForbidden, "config_not_writable", "config.yaml 不可写", nil)
			return
		}
	}

	updated, err := setup.ApplyPatch(s.cfg, patch)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_setup_config", err.Error(), nil)
		return
	}

	if patch.LLM != nil && len(patch.LLM.Profiles) > 0 {
		if err := s.persistLLMConfigs(r.Context(), patch.LLM.Profiles, updated.LLM.ActiveProfileID()); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "llm_config_save_failed", err.Error(), nil)
			return
		}
		if records, err := s.llmConfigs.List(r.Context()); err == nil {
			store.ApplyLLMConfigsToConfig(updated, records, updated.LLM.ActiveProfileID())
		}
	}

	if yamlWritable {
		if err := config.SaveFile(s.configPath, updated); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "config_save_failed", err.Error(), nil)
			return
		}
	} else if patch.LLM == nil || setup.PatchHasNonLLMBlock(patch) {
		writeAPIError(w, http.StatusForbidden, "config_not_writable", "config.yaml 不可写", nil)
		return
	}

	setup.CopyConfig(s.cfg, updated)
	s.syncLLMRuntimeFromStore(r.Context())
	s.applyMultimodalRuntime(s.cfg.MultimodalEnabled())
	if s.tools != nil {
		attachWeComRuntime(s.tools, s.cfg)
	}
	view := setup.ViewFromConfig(s.cfg)
	s.enrichLLMSettingsView(&view.LLM)
	view.ConfigPath = s.configPath
	view.ConfigWritable = yamlWritable || s.llmConfigs != nil
	view.RestartRequired = true
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) persistLLMConfigs(ctx context.Context, profiles []setup.LLMProfileSettings, activeID string) error {
	if s.llmConfigs == nil {
		return nil
	}
	existing := map[string]store.LLMConfigRecord{}
	if records, err := s.llmConfigs.List(ctx); err == nil {
		for _, rec := range records {
			existing[rec.ID] = rec
		}
	}
	records := make([]store.LLMConfigRecord, 0, len(profiles))
	keys := map[string]string{}
	clearIDs := map[string]bool{}
	for i, p := range profiles {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}
		thinking := strings.TrimSpace(p.Thinking)
		effort := strings.TrimSpace(p.ReasoningEffort)
		if old, ok := existing[id]; ok {
			if thinking == "" {
				thinking = old.Thinking
			}
			if effort == "" {
				effort = old.ReasoningEffort
			}
		}
		records = append(records, store.LLMConfigRecord{
			ID:                id,
			SortOrder:         i,
			Provider:          p.Provider,
			BaseURL:           p.BaseURL,
			Model:             p.Model,
			Mock:              p.Mock,
			Thinking:          thinking,
			ReasoningEffort:   effort,
			MultimodalEnabled: p.MultimodalEnabled,
		})
		if p.ClearAPIKey {
			clearIDs[id] = true
			continue
		}
		if key := strings.TrimSpace(p.APIKey); key != "" {
			keys[id] = key
			continue
		}
		// 兼容旧客户端：仍可提交 api_key_env，首次保存时从环境变量灌入。
		if env := strings.TrimSpace(p.APIKeyEnv); env != "" {
			if v := strings.TrimSpace(os.Getenv(env)); v != "" {
				keys[id] = v
			}
		}
	}
	_ = activeID
	return s.llmConfigs.ReplaceAll(ctx, records, keys, clearIDs)
}

func (s *Server) enrichLLMSettingsView(llm *setup.LLMSettings) {
	if llm == nil {
		return
	}
	if s.llmConfigs == nil {
		return
	}
	records, err := s.llmConfigs.List(context.Background())
	if err != nil || len(records) == 0 {
		return
	}
	hasKey := map[string]bool{}
	for _, rec := range records {
		hasKey[rec.ID] = rec.HasAPIKey()
	}
	for i := range llm.Profiles {
		llm.Profiles[i].APIKey = ""
		llm.Profiles[i].APIKeyEnv = ""
		llm.Profiles[i].HasAPIKey = hasKey[llm.Profiles[i].ID]
	}
	if llm.Active == "" && len(llm.Profiles) > 0 {
		llm.Active = llm.Profiles[0].ID
	}
}

func (s *Server) syncLLMRuntimeFromStore(ctx context.Context) {
	if s.llmRuntime == nil {
		return
	}
	s.llmRuntime.SyncFromConfig(s.cfg)
	if s.llmConfigs == nil {
		return
	}
	id := s.cfg.LLM.ActiveProfileID()
	if id == "" {
		id = s.cfg.LLM.FirstProfileID()
	}
	if id == "" {
		s.llmRuntime.SetAPIKey("")
		return
	}
	key, err := s.llmConfigs.ResolveAPIKey(ctx, id)
	if err != nil {
		s.logger.Warn("resolve llm api key failed", "id", id, "error", err)
		s.llmRuntime.SetAPIKey("")
		return
	}
	s.llmRuntime.SetAPIKey(key)
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
