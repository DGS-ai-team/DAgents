package api

import (
	"net/http"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
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
	writeJSON(w, http.StatusOK, s.llmRuntime.Snapshot())
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
	if patch.Thinking == nil && patch.ReasoningEffort == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_patch", "at least one of thinking or reasoning_effort is required", nil)
		return
	}
	view, err := s.llmRuntime.ApplyPatch(patch)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_llm_settings", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
