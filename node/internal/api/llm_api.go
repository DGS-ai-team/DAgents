package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func (s *Server) registerLLMRoutes() {
	s.mux.HandleFunc("GET /v1/llm/settings", s.handleGetLLMSettings)
	s.mux.HandleFunc("PATCH /v1/llm/settings", s.handlePatchLLMSettings)
}

func (s *Server) handleGetLLMSettings(w http.ResponseWriter, _ *http.Request) {
	if s.llmRuntime == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "llm_unavailable", "LLM runtime not configured", nil)
		return
	}
	writeJSON(w, http.StatusOK, s.llmSettingsView())
}

func (s *Server) handlePatchLLMSettings(w http.ResponseWriter, r *http.Request) {
	if s.llmRuntime == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "llm_unavailable", "LLM runtime not configured", nil)
		return
	}
	var patch llm.LLMSettingsPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if patch.ActiveProfile == nil && patch.Thinking == nil && patch.ReasoningEffort == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_patch", "at least one of active_profile, thinking or reasoning_effort is required", nil)
		return
	}

	if patch.ActiveProfile != nil {
		id := strings.TrimSpace(*patch.ActiveProfile)
		if id == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_llm_settings", "active_profile is required", nil)
			return
		}
		if err := s.switchActiveLLMProfile(id); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_llm_settings", err.Error(), nil)
			return
		}
	}

	if patch.Thinking != nil || patch.ReasoningEffort != nil {
		if _, err := s.llmRuntime.ApplyPatch(llm.LLMSettingsPatch{
			Thinking:        patch.Thinking,
			ReasoningEffort: patch.ReasoningEffort,
		}); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_llm_settings", err.Error(), nil)
			return
		}
		// 思考开关仅在状态栏控制；同步写入当前 LLM 配置，避免与连接设置互相覆盖。
		if err := s.persistActiveLLMThinking(r.Context()); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "llm_thinking_save_failed", err.Error(), nil)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.llmSettingsView())
}

func (s *Server) persistActiveLLMThinking(ctx context.Context) error {
	if s.cfg == nil || s.llmRuntime == nil {
		return nil
	}
	view := s.llmRuntime.Snapshot()
	if !view.ThinkingSupported {
		return nil
	}
	thinking := strings.TrimSpace(view.Thinking)
	effort := strings.TrimSpace(view.ReasoningEffort)
	if thinking == "" {
		thinking = "enabled"
	}
	if effort == "" {
		effort = "high"
	}

	updated := *s.cfg
	id := strings.TrimSpace(view.ActiveProfile)
	if id == "" {
		id = updated.LLM.ActiveProfileID()
	}
	if id == "" {
		id = updated.LLM.FirstProfileID()
	}

	updated.LLM.Thinking = thinking
	updated.LLM.ReasoningEffort = effort
	if id != "" {
		updated.LLM.Active = id
	}
	updated.SyncActiveProfileFromFlat()
	updated.ApplyDefaults()

	if s.llmConfigs != nil {
		id = updated.LLM.ActiveProfileID()
		records, err := s.llmConfigs.List(ctx)
		if err != nil {
			return err
		}
		if id != "" && len(records) > 0 {
			for i := range records {
				if records[i].ID != id {
					continue
				}
				records[i].Thinking = thinking
				records[i].ReasoningEffort = effort
			}
			if err := s.llmConfigs.ReplaceAll(ctx, records, nil, nil); err != nil {
				return err
			}
			store.ApplyLLMConfigsToConfig(&updated, records, id)
		}
	}

	if s.nodeSettings != nil {
		if err := s.nodeSettings.Save(ctx, &updated); err != nil {
			return err
		}
	}
	if s.configPath != "" && configPathWritable(s.configPath) {
		_ = config.SaveBootstrapFile(s.configPath, &updated)
	}
	setup.CopyConfig(s.cfg, &updated)
	return nil
}

func (s *Server) llmSettingsView() llm.LLMSettingsView {
	if s.llmRuntime == nil {
		return llm.LLMSettingsView{}
	}
	view := s.llmRuntime.Snapshot()
	if s.cfg != nil {
		view.ActiveProfile = s.cfg.LLM.ActiveProfileID()
		view.Profiles = s.cfg.LLM.ProfileIDs()
	}
	return view
}

func (s *Server) switchActiveLLMProfile(id string) error {
	if s.cfg == nil {
		return fmt.Errorf("llm config unavailable")
	}
	updated := *s.cfg
	if err := updated.SetActiveLLMProfile(id); err != nil {
		return err
	}
	updated.ApplyDefaults()
	if s.nodeSettings != nil {
		if err := s.nodeSettings.Save(context.Background(), &updated); err != nil {
			return err
		}
	}
	if s.configPath != "" && configPathWritable(s.configPath) {
		_ = config.SaveBootstrapFile(s.configPath, &updated)
	}
	setup.CopyConfig(s.cfg, &updated)
	s.syncLLMRuntimeFromStore(context.Background())
	s.applyMultimodalRuntime(s.cfg.MultimodalEnabled())
	return nil
}

func (s *Server) applyMultimodalRuntime(enabled bool) {
	if s.tools != nil {
		s.tools.SetMultimodalEnabled(enabled)
	}
	if s.sessions != nil {
		s.sessions.SetMultimodalEnabled(enabled)
	}
}
