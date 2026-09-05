package api

import (
	"context"
	"net/http"
	"strings"
)

func (s *Server) registerPlatformRoutes() {
	s.mux.HandleFunc("GET /v1/platform/capabilities", s.handlePlatformCapabilities)
	s.mux.HandleFunc("POST /v1/platform/directory-picker", s.handlePlatformDirectoryPicker)
	s.mux.HandleFunc("GET /v1/platform/clipboard/files", s.handlePlatformClipboardFiles)
	s.mux.HandleFunc("POST /v1/platform/ui-focus", s.handlePlatformUIFocus)
	s.mux.HandleFunc("POST /v1/platform/update/apply", s.handlePlatformUpdateApply)
}

type platformCapabilities struct {
	DesktopShell          bool `json:"desktop_shell"`
	NativeDirectoryPicker bool `json:"native_directory_picker"`
	ClipboardFilePaths    bool `json:"clipboard_file_paths"`
	WindowFocus           bool `json:"window_focus"`
	UpdateApply           bool `json:"update_apply"`
}

func (s *Server) platformAvailable(ctx context.Context) bool {
	return s != nil && s.desktopBridge != nil && s.desktopBridge.Available(ctx)
}

func (s *Server) handlePlatformCapabilities(w http.ResponseWriter, r *http.Request) {
	available := s.platformAvailable(r.Context())
	writeJSON(w, http.StatusOK, platformCapabilities{
		DesktopShell:          available,
		NativeDirectoryPicker: available,
		ClipboardFilePaths:    available,
		WindowFocus:           available,
		UpdateApply:           available,
	})
}

func (s *Server) handlePlatformDirectoryPicker(w http.ResponseWriter, r *http.Request) {
	if s.desktopBridge == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "desktop_unavailable", "当前运行环境不支持本机目录选择，请启动桌面 Shell", nil)
		return
	}
	out, err := s.desktopBridge.DirectoryPicker(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "desktop_unavailable", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePlatformClipboardFiles(w http.ResponseWriter, r *http.Request) {
	if s.desktopBridge == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "desktop_unavailable", "当前运行环境不支持读取桌面剪贴板文件", nil)
		return
	}
	out, err := s.desktopBridge.ClipboardFiles(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "desktop_unavailable", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePlatformUIFocus(w http.ResponseWriter, r *http.Request) {
	if s.desktopBridge == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "desktop_unavailable", "桌面 Shell 不可用", nil)
		return
	}
	var payload map[string]any
	if err := decodeJSON(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_focus_request", err.Error(), nil)
		return
	}
	if strings.TrimSpace(platformStringValue(payload["source_id"])) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_focus_request", "source_id is required", nil)
		return
	}
	out, err := s.desktopBridge.UIFocus(r.Context(), payload)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "desktop_unavailable", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePlatformUpdateApply(w http.ResponseWriter, r *http.Request) {
	if s.desktopBridge == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "desktop_unavailable", "当前运行环境不支持自动安装更新，请使用桌面 Shell", nil)
		return
	}
	var payload struct {
		Force bool `json:"force"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_update_request", err.Error(), nil)
		return
	}
	out, err := s.desktopBridge.ApplyUpdate(r.Context(), payload.Force)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "update_apply_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func platformStringValue(v any) string {
	s, _ := v.(string)
	return s
}
