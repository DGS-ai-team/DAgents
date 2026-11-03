package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/setup"
)

func (s *Server) registerLLMRoutes() {
	s.mux.HandleFunc("GET /v1/llm/settings", s.handleGetLLMSettings)
	s.mux.HandleFunc("PATCH /v1/llm/settings", s.handlePatchLLMSettings)
	s.mux.HandleFunc("GET /v1/llm/capabilities", s.handleGetLLMCapabilities)
}

func (s *Server) handleGetLLMCapabilities(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	model := r.URL.Query().Get("model")
	capability := llm.ModelCapabilities(provider, model)
	writeJSON(w, http.StatusOK, map[string]any{
		"supported":                capability.Control != "",
		"thinking_control":         capability.Control,
		"thinking_label":           capability.Label,
		"thinking_secondary_label": capability.SecondaryLabel,
		"allowed_efforts":          capability.AllowedEfforts,
		"default_effort":           capability.DefaultEffort,
		"can_disable_thinking":     capability.CanDisable,
		"thinking_budget_min":      capability.BudgetMin,
		"thinking_budget_max":      capability.BudgetMax,
	})
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
	s.llmRefMu.Lock()
	defer s.llmRefMu.Unlock()
	var patch llm.LLMSettingsPatch
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if patch.ActiveProfile == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_patch", "active_profile is required", nil)
		return
	}

	if patch.ActiveProfile != nil {
		id := strings.TrimSpace(*patch.ActiveProfile)
		if id == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_llm_settings", "active_profile is required", nil)
			return
		}
		if !s.setupWritable() {
			writeAPIError(w, http.StatusServiceUnavailable, "settings_store_unavailable", s.settingsWriteUnavailableMessage(), nil)
			return
		}
		if err := s.switchActiveLLMProfile(r.Context(), id); err != nil {
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

func (s *Server) switchActiveLLMProfile(ctx context.Context, id string) error {
	if s.cfg == nil {
		return fmt.Errorf("llm config unavailable")
	}
	if !s.setupWritable() {
		return fmt.Errorf("node settings and LLM config stores are required")
	}
	updated := *s.cfg
	if err := updated.SetActiveLLMProfile(id); err != nil {
		return err
	}
	updated.ApplyDefaults()
	if err := s.nodeSettings.Save(ctx, &updated); err != nil {
		return err
	}
	setup.CopyConfig(s.cfg, &updated)
	s.syncLLMRuntimeFromStore(ctx)
	s.applyMultimodalRuntime(s.cfg.MultimodalEnabled())
	return nil
}

func (s *Server) applyMultimodalRuntime(enabled bool) {
	// LLM 客户端为进程级共享；多模态工具开关只更新默认 Registry / 默认 TurnOptions，
	// 已装入 Agent 的开关在 ensure/reload 时按绑定档案重建，避免串改其他 Agent。
	if s.tools != nil {
		s.tools.SetMultimodalEnabled(enabled)
	}
	if s.sessions != nil {
		s.sessions.SetMultimodalEnabled(enabled)
	}
}
