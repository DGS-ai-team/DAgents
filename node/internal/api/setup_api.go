package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func (s *Server) registerSetupRoutes() {
	s.mux.HandleFunc("GET /v1/settings", s.handleGetSetupConfig)
	s.mux.HandleFunc("PATCH /v1/settings", s.handlePatchSetupConfig)
	s.mux.HandleFunc("POST /v1/llm/probe-models", s.handleProbeLLMModels)
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
	s.llmRefMu.Lock()
	defer s.llmRefMu.Unlock()

	if setup.PatchHasNonLLMBlock(patch) && s.nodeSettings == nil && !(s.configPath != "" && configPathWritable(s.configPath)) {
		writeAPIError(w, http.StatusForbidden, "config_not_writable", "node_settings 不可用且 config.yaml 不可写", nil)
		return
	}

	updated, err := setup.ApplyPatch(s.cfg, patch)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_setup_config", err.Error(), nil)
		return
	}

	// BrowserManager is a live process dependency rather than a passive config
	// value. Prepare and ping the replacement before persisting browser enable
	// or endpoint changes; otherwise the UI can report an enabled browser while
	// the Agent registry still has no executable browser tools.
	browserRuntimeChange := patch.Features != nil || patch.Browser != nil
	var preparedBrowserManager *browser.Manager
	var preparedBrowserService *browser.ServiceProcess
	if browserRuntimeChange && updated.BrowserEnabled() && !config.BrowserPlatformSupported() {
		writeAPIError(w, http.StatusNotImplemented, "unsupported_browser_platform", config.BrowserPlatformReason(), nil)
		return
	}
	if browserRuntimeChange && updated.BrowserEnabled() {
		preparedBrowserManager, preparedBrowserService, err = s.browserRuntimeFactory(updated)
		if err != nil {
			writeAPIError(w, http.StatusServiceUnavailable, "browser_service_unavailable", err.Error(), nil)
			return
		}
	}
	browserManagerCommitted := false
	if preparedBrowserManager != nil {
		defer func() {
			if !browserManagerCommitted {
				_ = preparedBrowserManager.Close()
				_ = preparedBrowserService.Close()
			}
		}()
	}

	needsLLMPersist := patch.LLM != nil && len(patch.LLM.Profiles) > 0 && s.llmConfigs != nil
	needsLLMReferenceCheck := patch.LLM != nil && len(patch.LLM.Profiles) > 0
	var llmSnapshot []store.LLMConfigRecord
	var settingsSnapshot *config.Config
	if needsLLMPersist {
		records, err := s.llmConfigs.List(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "llm_config_load_failed", err.Error(), nil)
			return
		}
		llmSnapshot = records
	}
	if needsLLMReferenceCheck {
		records := llmSnapshot
		if !needsLLMPersist {
			records = llmProfileRecordsFromConfig(s.cfg)
		}
		if err := s.validateLLMProfileReferences(r.Context(), records, patch.LLM.Profiles); err != nil {
			if inUse, ok := err.(*llmProfileInUseError); ok {
				writeAPIError(w, http.StatusConflict, "llm_profile_in_use", inUse.Error(), map[string]any{
					"profile_id": inUse.ProfileID,
					"agent_ids":  inUse.AgentIDs,
					"action":     "先修改这些 Agent 的模型配置，或删除这些 Agent 后重试",
				})
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "llm_profile_reference_check_failed", err.Error(), nil)
			return
		}
	}
	if s.nodeSettings != nil {
		prev, err := s.nodeSettings.Load(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "node_settings_load_failed", err.Error(), nil)
			return
		}
		if prev != nil {
			snap := *prev
			settingsSnapshot = &snap
		}
	}

	// node_settings 先于 llm_configs 写入；LLM 失败时回滚 settings，避免部分成功。
	if s.nodeSettings != nil {
		if err := s.nodeSettings.Save(r.Context(), updated); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "node_settings_save_failed", err.Error(), nil)
			return
		}
		if s.configPath != "" && configPathWritable(s.configPath) {
			if err := config.SaveBootstrapFile(s.configPath, updated); err != nil && s.logger != nil {
				s.logger.Warn("save bootstrap config failed", "path", s.configPath, "error", err)
			}
		}
	}

	if needsLLMPersist {
		if err := s.persistLLMConfigs(r.Context(), patch.LLM.Profiles); err != nil {
			if s.nodeSettings != nil && settingsSnapshot != nil {
				_ = s.nodeSettings.Save(r.Context(), settingsSnapshot)
			}
			writeAPIError(w, http.StatusInternalServerError, "llm_config_save_failed", err.Error(), nil)
			return
		}
		if records, err := s.llmConfigs.List(r.Context()); err == nil {
			store.ApplyLLMConfigsToConfig(updated, records, updated.LLM.ActiveProfileID())
		}
	}

	if s.nodeSettings != nil {
		// settings 已在上方持久化
	} else if s.configPath != "" && configPathWritable(s.configPath) {
		if err := config.SaveFile(s.configPath, updated); err != nil {
			if needsLLMPersist && len(llmSnapshot) > 0 {
				_ = s.llmConfigs.ReplaceAll(r.Context(), llmSnapshot, nil, nil)
			}
			writeAPIError(w, http.StatusInternalServerError, "config_save_failed", err.Error(), nil)
			return
		}
	} else if patch.LLM == nil || setup.PatchHasNonLLMBlock(patch) {
		writeAPIError(w, http.StatusForbidden, "config_not_writable", "无法持久化设置", nil)
		return
	}

	setup.CopyConfig(s.cfg, updated)
	if browserRuntimeChange {
		s.installBrowserRuntime(preparedBrowserManager, preparedBrowserService, updated.Browser.ServiceURL)
		browserManagerCommitted = true
	}
	s.syncLLMRuntimeFromStore(r.Context())
	s.applyMultimodalRuntime(s.cfg.MultimodalEnabled())
	s.attachNodeRuntimeDeps(s.tools, s.cfg.NodeID)
	s.maybeStartManageSidecars()
	view := setup.ViewFromConfig(s.cfg)
	s.enrichLLMSettingsView(&view.LLM)
	view.ConfigPath = s.configPath
	view.ConfigWritable = s.setupWritable()
	// Browser changes are applied to the live manager and loaded registries in
	// this request. Other settings retain the existing restart signal.
	view.RestartRequired = !browserRuntimeChange
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) persistLLMConfigs(ctx context.Context, profiles []setup.LLMProfileSettings) error {
	if s.llmConfigs == nil {
		return nil
	}
	existing := map[string]store.LLMConfigRecord{}
	existingRecords, err := s.llmConfigs.List(ctx)
	if err != nil {
		return fmt.Errorf("load existing llm configs: %w", err)
	}
	for _, rec := range existingRecords {
		existing[rec.ID] = rec
	}
	if err := s.validateLLMProfileReferences(ctx, existingRecords, profiles); err != nil {
		return err
	}

	// Validate secret moves before ReplaceAll starts its transaction. A blank
	// key on a renamed profile means "keep the old key", so ambiguity must fail
	// instead of silently dropping or attaching another profile's secret.
	destinations := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		if id := strings.TrimSpace(p.ID); id != "" {
			destinations[id] = true
		}
	}
	migrations := map[string]string{} // destination ID -> source ID
	sourceDestinations := map[string]int{}
	for _, p := range profiles {
		id := strings.TrimSpace(p.ID)
		sourceID := strings.TrimSpace(p.OriginalID)
		if id == "" || sourceID == "" || sourceID == id || p.ClearAPIKey || strings.TrimSpace(p.APIKey) != "" {
			continue
		}
		if _, destinationExists := existing[id]; destinationExists && id != sourceID {
			return fmt.Errorf("cannot preserve API key while renaming %q: destination %q already exists", sourceID, id)
		}
		if _, ok := existing[sourceID]; !ok {
			return fmt.Errorf("cannot preserve API key while renaming %q: source profile does not exist", sourceID)
		}
		if destinations[sourceID] {
			return fmt.Errorf("cannot preserve API key while renaming %q: source profile is still present", sourceID)
		}
		sourceDestinations[sourceID]++
		migrations[id] = sourceID
	}
	for sourceID, count := range sourceDestinations {
		if count > 1 {
			return fmt.Errorf("cannot preserve API key from %q: multiple profiles reference the same source", sourceID)
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
		old, ok := existing[id]
		if !ok {
			old, ok = existing[migrations[id]]
		}
		if ok {
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
		if sourceID := migrations[id]; sourceID != "" {
			source := existing[sourceID]
			if source.HasAPIKey() {
				key, err := s.llmConfigs.DecryptAPIKey(source)
				if err != nil {
					return fmt.Errorf("read API key for source profile %q: %w", sourceID, err)
				}
				keys[id] = key
				continue
			}
		}
		if env := strings.TrimSpace(p.APIKeyEnv); env != "" {
			if v := strings.TrimSpace(os.Getenv(env)); v != "" {
				keys[id] = v
			}
		}
	}
	return s.llmConfigs.ReplaceAll(ctx, records, keys, clearIDs)
}

type llmProfileInUseError struct {
	ProfileID string
	AgentIDs  []string
}

func (e *llmProfileInUseError) Error() string {
	return fmt.Sprintf("无法删除或改名模型配置「%s」：仍被 Agent「%s」使用。请先修改这些 Agent 的模型配置，或删除这些 Agent 后重试。", e.ProfileID, strings.Join(e.AgentIDs, "、"))
}

func llmProfileRecordsFromConfig(cfg *config.Config) []store.LLMConfigRecord {
	if cfg == nil {
		return nil
	}
	records := make([]store.LLMConfigRecord, 0, len(cfg.LLM.Profiles))
	for id := range cfg.LLM.Profiles {
		records = append(records, store.LLMConfigRecord{ID: id})
	}
	return records
}

func (s *Server) validateLLMProfileReferences(ctx context.Context, existing []store.LLMConfigRecord, profiles []setup.LLMProfileSettings) error {
	if s.agents == nil || len(existing) == 0 {
		return nil
	}
	retained := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		if id := strings.TrimSpace(profile.ID); id != "" {
			retained[id] = struct{}{}
		}
	}
	removed := make(map[string]struct{})
	for _, record := range existing {
		if _, ok := retained[record.ID]; !ok {
			removed[record.ID] = struct{}{}
		}
	}
	if len(removed) == 0 {
		return nil
	}
	agents, err := s.agents.List(ctx)
	if err != nil {
		return fmt.Errorf("list Agents while checking LLM config references: %w", err)
	}
	refs := make(map[string]map[string]struct{})
	for _, agent := range agents {
		snapshot, err := agentruntime.ParseSnapshot(agent.ConfigSnapshot)
		if err != nil {
			return fmt.Errorf("cannot verify LLM config references because Agent %q has an invalid config snapshot: %w", agent.AgentID, err)
		}
		id := agentruntime.LLMActiveFromDefaults(snapshot)
		if _, ok := removed[id]; !ok {
			continue
		}
		if refs[id] == nil {
			refs[id] = make(map[string]struct{})
		}
		refs[id][agent.AgentID] = struct{}{}
	}
	removedIDs := make([]string, 0, len(removed))
	for profileID := range removed {
		removedIDs = append(removedIDs, profileID)
	}
	sort.Strings(removedIDs)
	for _, profileID := range removedIDs {
		agentIDs := make([]string, 0, len(refs[profileID]))
		for id := range refs[profileID] {
			agentIDs = append(agentIDs, id)
		}
		if len(agentIDs) == 0 {
			continue
		}
		sort.Strings(agentIDs)
		return &llmProfileInUseError{ProfileID: profileID, AgentIDs: agentIDs}
	}
	return nil
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
	case llm.ProviderDeepSeek, llm.ProviderQwen, llm.ProviderOpenAI, llm.ProviderVLLM,
		llm.ProviderGLM, llm.ProviderMiniMax, llm.ProviderMiMo:
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
