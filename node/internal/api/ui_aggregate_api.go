package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/activity"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/version"
)

func (s *Server) registerUIAggregateRoutes() {
	s.mux.HandleFunc("GET /v1/ui/bootstrap", s.handleUIBootstrap)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/workspace-activity", s.handleAgentWorkspaceActivity)
}

// uiBootstrapResponse 聚合 Chat 首屏常用只读信息，减少并行 GET。
type uiBootstrapResponse struct {
	Health     healthResponse        `json:"health"`
	Info       agentInfoResponse     `json:"info"`
	LLM        llm.LLMSettingsView   `json:"llm"`
	User       uiBootstrapUser       `json:"user"`
	Onboarding uiBootstrapOnboarding `json:"onboarding"`
	Agent      uiBootstrapAgent      `json:"agent"`
}

type uiBootstrapUser struct {
	PreferredName string `json:"preferred_name"`
}

type uiBootstrapOnboarding struct {
	NodeProfileCompleted bool `json:"node_profile_completed"`
}

type uiBootstrapAgent struct {
	Name string `json:"name"`
}

func (s *Server) handleUIBootstrap(w http.ResponseWriter, _ *http.Request) {
	registered := false
	if s.registrar != nil {
		registered = s.registrar.Registered()
	}
	comp := compressionInfo{}
	if s.cfg != nil {
		comp.SilentTriggerTokens = s.cfg.Compression.SilentTriggerTokens
		comp.BlockingTriggerTokens = s.cfg.Compression.BlockingTriggerTokens
	}
	llmView := s.llmSettingsView()
	info := agentInfoResponse{
		NodeID:            "",
		Capabilities:      nil,
		MultimodalEnabled: false,
		ManageEnabled:     false,
		ManageURL:         "",
		ManageRegistered:  registered,
		LLM:               llmView,
		Compression:       comp,
	}
	user := uiBootstrapUser{}
	onboarding := uiBootstrapOnboarding{NodeProfileCompleted: true}
	agent := uiBootstrapAgent{}
	if s.cfg != nil {
		info.NodeID = s.cfg.NodeID
		info.Capabilities = s.cfg.Capabilities()
		info.MultimodalEnabled = s.cfg.MultimodalEnabled()
		info.ManageEnabled = s.cfg.Manage.Enabled
		info.ManageURL = strings.TrimSpace(s.cfg.Manage.URL)
		user.PreferredName = s.cfg.PreferredName()
		onboarding.NodeProfileCompleted = s.cfg.NodeProfileCompleted()
		agent.Name = strings.TrimSpace(s.cfg.Agent.Name)
	}
	writeJSON(w, http.StatusOK, uiBootstrapResponse{
		Health: healthResponse{
			Status:  "ok",
			NodeID:  info.NodeID,
			Version: version.Version,
		},
		Info:       info,
		LLM:        llmView,
		User:       user,
		Onboarding: onboarding,
		Agent:      agent,
	})
}

type workspaceActivityResponse struct {
	AgentID     string `json:"agent_id"`
	GeneratedAt string `json:"generated_at"`
	activity.Snapshot
}

func (s *Server) handleAgentWorkspaceActivity(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("agent_id")
		_, msgs, err := s.sessions.ContextSummary(id)
		if err != nil {
			if err.Error() == "agent_not_found" {
				writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在或尚未激活", map[string]any{"agent_id": id})
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
			return
		}
		snap := activity.DeriveFromMessages(msgs)
		if snap.Files == nil {
			snap.Files = []activity.FileChange{}
		}
		if snap.Commands == nil {
			snap.Commands = []activity.CommandExec{}
		}
		writeJSON(w, http.StatusOK, workspaceActivityResponse{
			AgentID:     id,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Snapshot:    snap,
		})
	})(w, r)
}
