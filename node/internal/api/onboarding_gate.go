package api

import (
	"net/http"
)

// onboardingGateMiddleware 在 Node 身份首配完成前拦截业务 API。
// 仅放行探活、Node 信息、bootstrap、settings 与 LLM 探测。
func (s *Server) onboardingGateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.cfg == nil || s.cfg.NodeProfileCompleted() {
			next.ServeHTTP(w, r)
			return
		}
		// Let ServeMux preserve its normal 404/405 behavior for paths that are
		// not registered. The onboarding gate only applies to actual API routes.
		if _, pattern := s.mux.Handler(r); pattern == "" {
			next.ServeHTTP(w, r)
			return
		}
		if onboardingPathAllowed(r.Method, r.URL.Path) &&
			(r.URL.Path != "/v1/node/upgrade-readiness" || isLoopbackRequest(r)) {
			next.ServeHTTP(w, r)
			return
		}
		writeAPIError(w, http.StatusForbidden, "node_profile_required",
			"请先完成 Node 身份与 LLM 配置后再使用本机功能", nil)
	})
}

func onboardingPathAllowed(method, path string) bool {
	switch {
	case method == http.MethodGet && path == "/health":
		return true
	case method == http.MethodGet && path == "/v1/node/info":
		return true
	case method == http.MethodGet && path == "/v1/node/upgrade-readiness":
		return true
	case method == http.MethodGet && path == "/v1/bootstrap":
		return true
	case method == http.MethodGet && path == "/v1/settings":
		return true
	case method == http.MethodPatch && path == "/v1/settings":
		return true
	case method == http.MethodPost && path == "/v1/llm/probe-models":
		return true
	default:
		return false
	}
}
