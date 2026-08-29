package api

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap keeps optional HTTP interfaces (notably Hijacker) available to
// protocol handlers such as WebSocket upgrades while still recording status.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	if r.ResponseWriter != nil {
		r.ResponseWriter.WriteHeader(code)
	}
}

// accessLogMiddleware 记录 HTTP 请求 method/path/status/耗时；/health 仅在 Debug 级别输出。
func accessLogMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		durMS := time.Since(start).Milliseconds()
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"query_keys", queryKeys(r),
			"status", rec.status,
			"duration_ms", durMS,
		}
		if r.URL.Path == "/health" {
			logger.Debug("http request", attrs...)
		} else if rec.status >= 500 {
			logger.Error("http request", attrs...)
		} else if rec.status >= 400 {
			logger.Warn("http request", attrs...)
		} else {
			logger.Info("http request", attrs...)
		}
	})
}

// queryKeys keeps request observability while ensuring access logs never copy
// query-string values, which may contain tokens or user-provided content.
func queryKeys(r *http.Request) []string {
	if r == nil || r.URL == nil {
		return nil
	}
	values := r.URL.Query()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
