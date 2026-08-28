package api

import (
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/version"
)

func (s *Server) registerUIAggregateRoutes() {
	s.mux.HandleFunc("GET /v1/ui/bootstrap", s.handleUIBootstrap)
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
