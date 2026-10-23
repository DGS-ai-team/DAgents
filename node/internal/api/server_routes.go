package api

// registerRoutes 构建 Node 的 HTTP 路由树。
// 各领域路由仍由对应文件实现，Server 只负责统一挂载顺序。
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/node/info", s.handleAgentInfo)
	s.mux.HandleFunc("GET /v1/node/update", s.handleAgentUpdate)
	s.mux.HandleFunc("GET /v1/node/update/package", s.handleNodeUpdatePackage)
	s.mux.HandleFunc("GET /v1/node/upgrade-readiness", s.handleAgentUpgradeReadiness)
	s.registerAgentRoutes()
	s.registerBrowserViewRoutes()
	s.registerAutonomyV2Routes()
	s.registerDreamingRoutes()
	s.mux.HandleFunc("GET /v1/auto/overview", s.handleAutoOverview)
	s.registerMCPRoutes()
	s.registerLinuxChannelRoutes()
	s.registerTerminalRoutes()
	s.registerWorkgroupRoutes()
	s.registerScreenRoutes()
	s.registerToolCallControlRoutes()
	s.registerLinuxTransferRoutes()
	s.registerUIAggregateRoutes()
	s.registerPlatformRoutes()
	s.mux.HandleFunc("POST /v1/messages", s.handlePostMessage)
	s.mux.HandleFunc("GET /v1/streams", s.handleStreams)
	s.registerTriggerRoutes()
	s.registerMediaRoutes()
	s.registerLLMRoutes()
	s.registerSetupRoutes()
	s.registerManageUploadRoutes()
	s.registerFeedbackRoutes()
	s.mux.HandleFunc("GET /v1/skills/catalog", s.handleNodeSkillsCatalog)
}
