package api

import (
	"net/http"
	"strings"
)

// onboardingGateMiddleware 在 Node 身份首配完成前拦截业务 API。
// 仅放行探活、bootstrap、setup 读写与 /ui 静态资源（供首配页加载）。
func (s *Server) onboardingGateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.cfg == nil || s.cfg.NodeProfileCompleted() {
			next.ServeHTTP(w, r)
			return
		}
		if onboardingPathAllowed(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		writeAPIError(w, http.StatusForbidden, "node_profile_required",
			"请先完成 Node 身份配置后再使用本机功能", nil)
	})
}

func onboardingPathAllowed(method, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	switch {
	case method == http.MethodGet && path == "/health":
		return true
	case method == http.MethodGet && path == "/v1/ui/bootstrap":
		return true
	case method == http.MethodGet && path == "/v1/setup/config":
		return true
	case method == http.MethodPatch && path == "/v1/setup/config":
		return true
	case method == http.MethodGet && (path == "/ui" || path == "/ui/" || strings.HasPrefix(path, "/ui/")):
		return true
	default:
		return false
	}
}
