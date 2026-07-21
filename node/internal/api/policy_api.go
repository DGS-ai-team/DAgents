package api

import (
	"net/http"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

func (s *Server) registerPolicyRoutes() {
	// 全局 /v1/policy 已下线；策略按 Agent 存于 agents.db，见 /v1/agents/{id}/policy*。
	s.mux.HandleFunc("GET /v1/policy", s.handleDeprecatedGlobalPolicy)
	s.mux.HandleFunc("PUT /v1/policy/tools", s.handleDeprecatedGlobalPolicy)
	s.mux.HandleFunc("PUT /v1/policy/shell/{shell_type}", s.handleDeprecatedGlobalPolicy)
}

func (s *Server) handleDeprecatedGlobalPolicy(w http.ResponseWriter, _ *http.Request) {
	writeAPIError(w, http.StatusGone, "policy_moved",
		"全局 /v1/policy 已下线；请使用 GET/PUT /v1/agents/{agent_id}/policy（SQLite 按 Agent 存储）",
		map[string]any{"replacement": "/v1/agents/{agent_id}/policy"},
	)
}

type policyToolUpdatesBody struct {
	Updates []policy.ToolUpdate `json:"updates"`
}

type policyShellUpdatesBody struct {
	Updates []policy.ShellUpdate `json:"updates"`
	Deletes []string             `json:"deletes"`
}
