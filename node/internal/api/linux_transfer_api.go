package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) registerLinuxTransferRoutes() {
	s.mux.HandleFunc("GET /v1/transfers", s.handleLinuxTransfers)
	s.mux.HandleFunc("POST /v1/transfers/{transfer_id}/cancel", s.handleLinuxTransferCancel)
	s.mux.HandleFunc("GET /v1/transfers/events", s.handleLinuxTransferEvents)
}

func (s *Server) handleLinuxTransfers(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.transfers == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"transfers":            []any{},
			"max_concurrent_files": 0,
		})
		return
	}
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	writeJSON(w, http.StatusOK, map[string]any{
		"transfers":            s.transfers.List(agentID),
		"max_concurrent_files": s.transfers.MaxConcurrent(),
	})
}

func (s *Server) handleLinuxTransferCancel(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.transfers == nil {
		writeAPIError(w, http.StatusNotFound, "transfer_unavailable", "linux file transfer is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("transfer_id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_transfer", "transfer_id is required", nil)
		return
	}
	if !s.transfers.Cancel(id) {
		writeAPIError(w, http.StatusConflict, "transfer_not_running", "transfer does not exist or has already finished", map[string]any{
			"transfer_id": id,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"cancelled":   true,
		"transfer_id": id,
	})
}

func (s *Server) handleLinuxTransferEvents(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.transferStream == nil {
		writeAPIError(w, http.StatusNotFound, "transfer_unavailable", "linux file transfer is not configured", nil)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "streaming not supported", nil)
		return
	}
	lastSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))
	if after := strings.TrimSpace(r.URL.Query().Get("after_seq")); after != "" {
		lastSeq = parseLastEventID(after)
	}
	if strings.TrimSpace(r.URL.Query().Get("live")) == "1" {
		lastSeq = s.transferStream.CurrentSeq()
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events := s.transferStream.Subscribe(lastSeq)
	defer s.transferStream.Unsubscribe(events)
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if _, err := fmt.Fprint(w, ev.FormatSSE()); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
