package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/media"
)

func wantsMediaThumbnail(r *http.Request) bool {
	v := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("thumbnail")))
	return v == "1" || v == "true" || v == "yes"
}

func (s *Server) registerMediaRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/media/{media_id}", s.handleGetAgentMedia)
}

func (s *Server) handleGetAgentMedia(w http.ResponseWriter, r *http.Request) {
	s.withAgentAsSession(s.handleGetSessionMedia)(w, r)
}

func (s *Server) handleGetSessionMedia(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	mediaID := strings.TrimSpace(r.PathValue("media_id"))
	if sessionID == "" || mediaID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "session_id 与 media_id 必填", nil)
		return
	}
	art, absPath, err := s.sessions.OpenSessionMedia(sessionID, mediaID)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "media_not_found", "媒体不存在或 session 未加载", nil)
			return
		}
		writeAPIError(w, http.StatusBadRequest, "media_unavailable", err.Error(), nil)
		return
	}
	f, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeAPIError(w, http.StatusNotFound, "media_not_found", "文件已删除", nil)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "media_read_failed", err.Error(), nil)
		return
	}
	defer f.Close()
	if wantsMediaThumbnail(r) {
		if data, contentType, served, thumbErr := media.ThumbnailFromReader(f, art.MIME); thumbErr == nil && served {
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "private, max-age=3600")
			_, _ = w.Write(data)
			return
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "media_read_failed", err.Error(), nil)
			return
		}
	}
	info, err := f.Stat()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "media_read_failed", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", art.MIME)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, art.RelPath, info.ModTime(), f)
}
