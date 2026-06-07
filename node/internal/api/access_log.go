package api

import (
	"log/slog"
	"net/http"
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
			"query", r.URL.RawQuery,
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
