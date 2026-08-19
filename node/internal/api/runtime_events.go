package api

import (
	"context"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func (s *Server) markRuntimeReloadPending(agentID, reason string) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(agentID)
	if id == "" {
		return
	}
	s.runtimeReloadMu.Lock()
	if s.pendingRuntimeReload == nil {
		s.pendingRuntimeReload = make(map[string]string)
	}
	s.pendingRuntimeReload[id] = strings.TrimSpace(reason)
	s.runtimeReloadMu.Unlock()
}

func (s *Server) hasRuntimeReloadPending(agentID string) bool {
	if s == nil {
		return false
	}
	s.runtimeReloadMu.Lock()
	_, ok := s.pendingRuntimeReload[strings.TrimSpace(agentID)]
	s.runtimeReloadMu.Unlock()
	return ok
}

func (s *Server) clearRuntimeReloadPending(agentID string) {
	if s == nil {
		return
	}
	s.runtimeReloadMu.Lock()
	delete(s.pendingRuntimeReload, strings.TrimSpace(agentID))
	s.runtimeReloadMu.Unlock()
}

// reloadAgentRuntimeIfIdle preserves the active Turn snapshot. Catalog
// changes are applied immediately when idle, otherwise they are queued for
// the next ensure/reload boundary.
func (s *Server) reloadAgentRuntimeIfIdle(ctx context.Context, rec store.AgentRecord, reason string) (bool, error) {
	id := strings.TrimSpace(rec.AgentID)
	if s.sessions != nil {
		if _, active, state, _ := s.sessions.RuntimeInfo(id); active {
			s.markRuntimeReloadPending(id, reason)
			s.logger.Info("runtime reload deferred until turn idle", "agent_id", id, "reason", reason, "turn_state", state)
			return false, nil
		}
	}
	if err := s.reloadAgentRuntime(ctx, rec); err != nil {
		s.markRuntimeReloadPending(id, reason)
		return false, err
	}
	s.clearRuntimeReloadPending(id)
	return true, nil
}

func (s *Server) publishRuntimeConfigChanged(agentID, reason string, applied bool) {
	if s == nil || s.stream == nil {
		return
	}
	id := strings.TrimSpace(agentID)
	data := map[string]any{
		"agent_id": id,
		"reason":   strings.TrimSpace(reason),
		"applied":  applied,
	}
	if s.sessions != nil {
		data["runtime_revision"] = s.sessions.RuntimeRevision(id)
		data["runtime_digest"] = s.sessions.RuntimeDigest(id)
	}
	s.stream.Publish(id, "runtime/config-changed", data)
}

func (s *Server) publishMemoryChanged(agentID, reason string, entryCount int, nextTurn bool) {
	if s == nil || s.stream == nil {
		return
	}
	s.stream.Publish(strings.TrimSpace(agentID), "memory/changed", map[string]any{
		"agent_id":    strings.TrimSpace(agentID),
		"reason":      strings.TrimSpace(reason),
		"entry_count": entryCount,
		"next_turn":   nextTurn,
		"runtime_digest": func() string {
			if s.sessions == nil {
				return ""
			}
			return s.sessions.RuntimeDigest(strings.TrimSpace(agentID))
		}(),
	})
}
