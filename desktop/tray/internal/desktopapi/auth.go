package desktopapi

import (
	"net/http"
	"strings"
)

func withBridgeAuth(next http.Handler, token string) http.Handler {
	if strings.TrimSpace(token) == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.TrimSpace(r.Header.Get("Authorization")) != "Bearer "+token {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "desktop bridge authorization required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
