package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/setup"
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
	}
	writeJSON(w, http.StatusOK, s.llmSettingsView())
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
	if s.configPath != "" && configPathWritable(s.configPath) {
		if err := config.SaveFile(s.configPath, &updated); err != nil {
			return err
		}
	}
	setup.CopyConfig(s.cfg, &updated)
	s.llmRuntime.SyncFromConfig(s.cfg)
	return nil
}
