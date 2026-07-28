package api

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func (s *Server) registerSetupRoutes() {
	s.mux.HandleFunc("GET /v1/setup/config", s.handleGetSetupConfig)
	s.mux.HandleFunc("PATCH /v1/setup/config", s.handlePatchSetupConfig)
	s.mux.HandleFunc("POST /v1/setup/llm/probe-models", s.handleProbeLLMModels)
}

func (s *Server) setupWritable() bool {
	return s.nodeSettings != nil || s.llmConfigs != nil || (s.configPath != "" && configPathWritable(s.configPath))
}

func (s *Server) handleGetSetupConfig(w http.ResponseWriter, _ *http.Request) {
	view := setup.ViewFromConfig(s.cfg)
	s.enrichLLMSettingsView(&view.LLM)
	view.ConfigPath = s.configPath
	view.ConfigWritable = s.setupWritable()
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handlePatchSetupConfig(w http.ResponseWriter, r *http.Request) {
	if !s.setupWritable() {
		if s.configPath == "" && s.nodeSettings == nil && s.llmConfigs == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "config_path_unknown", "Node 未记录配置存储，无法保存", nil)
			return
		}
		writeAPIError(w, http.StatusForbidden, "config_not_writable", "配置不可写", nil)
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

	if setup.PatchHasNonLLMBlock(patch) && s.nodeSettings == nil && !(s.configPath != "" && configPathWritable(s.configPath)) {
		writeAPIError(w, http.StatusForbidden, "config_not_writable", "node_settings 不可用且 config.yaml 不可写", nil)
		return
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

	if s.nodeSettings != nil {
		if err := s.nodeSettings.Save(r.Context(), updated); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "node_settings_save_failed", err.Error(), nil)
			return
		}
		if s.configPath != "" && configPathWritable(s.configPath) {
			_ = config.SaveBootstrapFile(s.configPath, updated)
		}
	} else if s.configPath != "" && configPathWritable(s.configPath) {
		if err := config.SaveFile(s.configPath, updated); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "config_save_failed", err.Error(), nil)
			return
		}
	} else if patch.LLM == nil || setup.PatchHasNonLLMBlock(patch) {
		writeAPIError(w, http.StatusForbidden, "config_not_writable", "无法持久化设置", nil)
		return
	}

	setup.CopyConfig(s.cfg, updated)
	s.syncLLMRuntimeFromStore(r.Context())
	s.applyMultimodalRuntime(s.cfg.MultimodalEnabled())
	s.attachNodeRuntimeDeps(s.tools, s.cfg.NodeID)
	view := setup.ViewFromConfig(s.cfg)
	s.enrichLLMSettingsView(&view.LLM)
	view.ConfigPath = s.configPath
	view.ConfigWritable = s.setupWritable()
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

type probeLLMModelsRequest struct {
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	Provider  string `json:"provider"`
	ProfileID string `json:"profile_id"`
}

func (s *Server) handleProbeLLMModels(w http.ResponseWriter, r *http.Request) {
	var req probeLLMModelsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "base_url is required", nil)
		return
	}
	providerName := strings.ToLower(strings.TrimSpace(req.Provider))
	if providerName == "mock" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "Mock 模式无需探测模型列表", nil)
		return
	}
	apiKey := strings.TrimSpace(req.APIKey)
	profileID := strings.TrimSpace(req.ProfileID)
	if apiKey == "" && profileID != "" && s.llmConfigs != nil {
		key, err := s.llmConfigs.ResolveAPIKey(r.Context(), profileID)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "llm_key_unavailable", "无法读取已保存的 API Key: "+err.Error(), nil)
			return
		}
		apiKey = strings.TrimSpace(key)
	}
	provider := llm.ProviderName(providerName)
	switch provider {
	case llm.ProviderDeepSeek, llm.ProviderQwen, llm.ProviderOpenAI, llm.ProviderVLLM:
	default:
		if sug := llm.SuggestProviderFromBaseURL(baseURL); sug != "" {
			provider = llm.ProviderName(sug)
		} else {
			provider = llm.ProviderOpenAI
		}
	}
	result, err := llm.ProbeModels(r.Context(), provider, baseURL, apiKey)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "probe_models_failed", err.Error(), nil)
		return
	}
	if result.SuggestedProvider == "" {
		result.SuggestedProvider = string(provider)
	}
	writeJSON(w, http.StatusOK, result)
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
