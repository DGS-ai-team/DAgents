// Package desktopapi 提供 Shell localhost 辅助 HTTP API（update、clipboard、ui focus 等）。
package desktopapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/clipboard"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/uifocus"
	shellupdate "github.com/DGS-ai-team/DAgents/desktop/tray/internal/update"
	sharedupdate "github.com/DGS-ai-team/DAgents/shared/update"
)

// DefaultListenAddr 为 Shell localhost API 默认监听地址（与 browser 18766、Node 18765 错开）。
const DefaultListenAddr = "127.0.0.1:18767"

// UpdateProvider 暴露缓存的更新状态。
type UpdateProvider interface {
	Snapshot() sharedupdate.Status
}

// Server 提供 Shell localhost HTTP API。
type Server struct {
	addr    string
	updates UpdateProvider
	applier *shellupdate.Applier
	uiFocus *uifocus.Store
	token   string
	mux     *http.ServeMux
	srv     *http.Server

	mu sync.Mutex
}

// New 构造 localhost API 服务；updates 可为 nil（返回空状态）。
func New(updates UpdateProvider, applier *shellupdate.Applier, uiFocus *uifocus.Store, token ...string) *Server {
	if updates == nil {
		updates = shellupdate.DisabledProvider{}
	}
	s := &Server{
		addr:    DefaultListenAddr,
		updates: updates,
		applier: applier,
		uiFocus: uiFocus,
		mux:     http.NewServeMux(),
	}
	if len(token) > 0 {
		s.token = strings.TrimSpace(token[0])
	}
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/desktop/update", s.handleDesktopUpdate)
	s.mux.HandleFunc("POST /v1/desktop/update/apply", s.handleDesktopUpdateApply)
	s.mux.HandleFunc("GET /v1/desktop/clipboard/files", s.handleClipboardFiles)
	s.mux.HandleFunc("POST /v1/desktop/dialog/directory", s.handleDirectoryPicker)
	s.mux.HandleFunc("POST /v1/desktop/ui/focus", s.handleUIFocus)
	return s
}

// Handler 返回带来源校验和 bridge 认证的 HTTP handler。浏览器不应直接
// 调用此服务；Node 通过 Authorization 访问，localhost CORS 仅保留迁移期诊断能力。
func (s *Server) Handler() http.Handler {
	return withBridgeAuth(withLocalhostCORS(s.mux), s.token)
}

// Start 在后台监听；ctx 取消时优雅关闭。
func (s *Server) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.srv = &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.mu.Unlock()

	go func() {
		log.Printf("desktop API listening on http://%s", s.addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("desktop API: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.mu.Lock()
		srv := s.srv
		s.mu.Unlock()
		if srv != nil {
			_ = srv.Shutdown(shutdownCtx)
		}
	}()
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDesktopUpdate(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.updates.Snapshot())
}

func (s *Server) handleClipboardFiles(w http.ResponseWriter, _ *http.Request) {
	paths, err := clipboard.FilePaths()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"paths":   []string{},
			"message": err.Error(),
		})
		return
	}
	if paths == nil {
		paths = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"paths": paths})
}

func (s *Server) handleDirectoryPicker(w http.ResponseWriter, _ *http.Request) {
	path, err := pickDirectory()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"ok":        false,
			"cancelled": false,
			"path":      nil,
			"message":   err.Error(),
		})
		return
	}
	if strings.TrimSpace(path) == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"cancelled": true,
			"path":      nil,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"cancelled": false,
		"path":      path,
	})
}

func (s *Server) handleUIFocus(w http.ResponseWriter, r *http.Request) {
	var req uiFocusRequest
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"ok":      false,
				"message": "invalid json: " + err.Error(),
			})
			return
		}
	}
	if strings.TrimSpace(req.SourceID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":      false,
			"message": "source_id is required",
		})
		return
	}
	ttl := uifocus.DefaultTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	if s.uiFocus != nil {
		s.uiFocus.Report(req.SourceID, req.AgentID, ttl)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"agent_id":  req.AgentID,
		"source_id": req.SourceID,
	})
}

func (s *Server) handleDesktopUpdateApply(w http.ResponseWriter, r *http.Request) {
	var req applyRequest
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, applyResponse{
				OK:      false,
				Message: "invalid json: " + err.Error(),
			})
			return
		}
	}
	if s.applier == nil {
		writeJSON(w, http.StatusServiceUnavailable, applyResponse{
			OK:      false,
			Message: "update apply unavailable",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
	defer cancel()
	result, code := s.applier.Run(ctx, shellupdate.ApplyOptions{
		Force:       req.Force,
		SkipConfirm: true,
	})
	status := http.StatusOK
	writeJSON(w, status, applyResponse{
		OK:      code == 0 || code == shellupdate.ExitUpToDate,
		Message: result.Message,
		Code:    code,
		Status:  result.Status,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type uiFocusRequest struct {
	AgentID    string `json:"agent_id"`
	SourceID   string `json:"source_id,omitempty"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}
