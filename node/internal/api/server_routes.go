package api

import "github.com/DGS-ai-team/DAgents/node/internal/webui"

// registerRoutes 构建 Node 的 HTTP 路由树。
// 各领域路由仍由对应文件实现，Server 只负责统一挂载顺序。
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /v1/desktop/ui/focus", s.handleDesktopUIFocus)
	s.mux.HandleFunc("GET /v1/agent/info", s.handleAgentInfo)
	s.mux.HandleFunc("GET /v1/agent/update", s.handleAgentUpdate)
	s.mux.HandleFunc("GET /v1/agent/upgrade-readiness", s.handleAgentUpgradeReadiness)
	s.registerAgentRoutes()
	s.registerMCPRoutes()
	s.registerLinuxChannelRoutes()
	s.registerTerminalRoutes()
	s.registerWorkgroupRoutes()
	s.registerScreenRoutes()
	s.registerToolCallControlRoutes()
	s.registerLinuxTransferRoutes()
	s.registerUIAggregateRoutes()
	s.mux.HandleFunc("POST /v1/messages", s.handlePostMessage)
	s.mux.HandleFunc("GET /v1/streams", s.handleStreams)
	s.registerTriggerRoutes()
	s.registerMediaRoutes()
	s.registerLLMRoutes()
	s.registerSetupRoutes()
	s.registerManageUploadRoutes()
	s.mux.HandleFunc("GET /v1/skills/catalog", s.handleNodeSkillsCatalog)
	s.mux.Handle("GET /ui/", webui.Handler())
	s.mux.HandleFunc("GET /ui", webui.RedirectHandler())
}
