package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/screen"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func (s *Server) registerScreenRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/screen/stream", s.handleAgentScreenStream)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/screen/status", s.handleAgentScreenStatus)
}

func (s *Server) handleAgentScreenStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if s.agents != nil {
		rec, err := s.agents.Get(r.Context(), id)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
			return
		}
		if rec == nil || rec.Archived {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
			return
		}
		// 远端引用：D5 已停 Edge；返回 placement_deprecated。
		if store.NormalizeAgentOrigin(rec.Origin) == store.AgentOriginRemote {
			writeRemotePlacementDeprecated(w, id)
			return
		}
	}
	st := screen.Default().Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": id,
		"host":     st,
	})
}

func (s *Server) handleAgentScreenStream(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if s.agents != nil {
		rec, err := s.agents.Get(r.Context(), id)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
			return
		}
		if rec == nil || rec.Archived {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
			return
		}
		if store.NormalizeAgentOrigin(rec.Origin) == store.AgentOriginRemote {
			writeRemotePlacementDeprecated(w, id)
			return
		}
	}

	cap := screen.Default()
	st := cap.Status()
	if !st.Available {
		writeAPIError(w, http.StatusNotFound, "screen_unavailable",
			"当前主机无可旁观屏幕",
			map[string]any{"agent_id": id, "backend": st.Backend, "reason": st.Reason})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "streaming not supported", nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	statusPayload, _ := json.Marshal(map[string]any{
		"display_available": st.Available,
		"backend":           st.Backend,
		"display_label":     st.Label,
		"agent_id":          id,
	})
	if _, err := fmt.Fprintf(w, "event: status\ndata: %s\n\n", statusPayload); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(screen.MinFrameInterval())
	defer ticker.Stop()
	ctx := r.Context()

	sendFrame := func() bool {
		frame, err := cap.Capture(ctx)
		if err != nil {
			if errors.Is(err, screen.ErrUnavailable) {
				payload, _ := json.Marshal(map[string]any{"error": "screen_unavailable"})
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
				flusher.Flush()
			}
			return false
		}
		payload, _ := json.Marshal(map[string]any{
			"ts":   frame.At.UnixMilli(),
			"w":    frame.Width,
			"h":    frame.Height,
			"mime": frame.Mime,
			"b64":  base64.StdEncoding.EncodeToString(frame.JPEG),
		})
		if _, err := fmt.Fprintf(w, "event: frame\ndata: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// 立即发首帧
	if !sendFrame() {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !sendFrame() {
				return
			}
		}
	}
}
