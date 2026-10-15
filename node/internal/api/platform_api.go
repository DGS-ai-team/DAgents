package api

import (
	"net/http"
)

func (s *Server) registerPlatformRoutes() {
	s.mux.HandleFunc("GET /v1/platform/capabilities", s.handlePlatformCapabilities)
	s.mux.HandleFunc("POST /v1/platform/directory-picker", s.handlePlatformDirectoryPicker)
}

// platformCapabilities describes Node-host capabilities only. Desktop shell
// capabilities are exposed by the Desktop IPC bridge, never proxied through Node.
type platformCapabilities struct {
	NativeDirectoryPicker bool `json:"native_directory_picker"`
}

func (s *Server) handlePlatformCapabilities(w http.ResponseWriter, r *http.Request) {
	nativeDirectoryAvailable := s.directoryPicker != nil && s.directoryPicker.Available(r.Context())
	writeJSON(w, http.StatusOK, platformCapabilities{NativeDirectoryPicker: nativeDirectoryAvailable})
}

func (s *Server) handlePlatformDirectoryPicker(w http.ResponseWriter, r *http.Request) {
	if s.directoryPicker == nil || !s.directoryPicker.Available(r.Context()) {
		writeAPIError(w, http.StatusServiceUnavailable, "native_directory_picker_unavailable", "当前 Node 宿主不支持本机目录选择，请直接输入绝对路径", nil)
		return
	}
	out, err := s.directoryPicker.Pick(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "native_directory_picker_unavailable", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
