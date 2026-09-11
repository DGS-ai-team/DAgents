package session

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type activeContextBoundary struct {
	SessionID string `json:"session_id"`
	Revision  uint64 `json:"revision"`
	Index     int    `json:"index"`
	Prefix    string `json:"prefix"`
}

func activeContextPrefixDigest(messages []llm.Message, index int) string {
	if index < 0 || index > len(messages) {
		return ""
	}
	raw, err := json.Marshal(messages[:index])
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:])
}

// CaptureActiveContextBoundary returns the opaque boundary fixed for one
// dreaming attempt. Messages appended after capture remain in the next
// active segment and cannot be discarded by a later retry.
func (m *Manager) CaptureActiveContextBoundary(ctx context.Context, sessionID string) (string, error) {
	sid := strings.TrimSpace(sessionID)
	r := m.getRuntime(sid)
	if r == nil || r.executionGate == nil || !r.executionGate.owns(ctx) {
		return "", fmt.Errorf("maintenance lease required")
	}
	r.mu.Lock()
	b, err := json.Marshal(activeContextBoundary{SessionID: sid, Revision: r.historyRevision, Index: len(r.messages), Prefix: activeContextPrefixDigest(r.messages, len(r.messages))})
	r.mu.Unlock()
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func decodeActiveContextBoundary(raw, sessionID string) (activeContextBoundary, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return activeContextBoundary{}, fmt.Errorf("invalid context boundary")
	}
	var out activeContextBoundary
	if err := json.Unmarshal(b, &out); err != nil || out.SessionID != strings.TrimSpace(sessionID) || out.Index < 0 {
		return activeContextBoundary{}, fmt.Errorf("invalid context boundary")
	}
	return out, nil
}

// ResetActiveContext starts a new model-visible context segment while keeping
// the complete durable transcript available to hydrate/history callers. The
// caller must hold the Agent's maintenance lease; resetID is the durable
// idempotency key for one dreaming commit.
func (m *Manager) ResetActiveContext(ctx context.Context, sessionID, boundary, resetID string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sid := strings.TrimSpace(sessionID)
	rid := strings.TrimSpace(resetID)
	if sid == "" || rid == "" || strings.TrimSpace(boundary) == "" {
		return false, fmt.Errorf("session_id, boundary and reset_id are required")
	}
	r := m.getRuntime(sid)
	if r == nil {
		return false, fmt.Errorf("agent_not_found")
	}
	if r.executionGate == nil || !r.executionGate.owns(ctx) {
		return false, fmt.Errorf("maintenance lease required")
	}
	b, err := decodeActiveContextBoundary(boundary, sid)
	if err != nil {
		return false, err
	}
	if state := r.turnCoordinator.Snapshot(); state.HasActiveTurn && !state.TurnStatus.Terminal() {
		return false, fmt.Errorf("session is busy")
	}
	r.mu.Lock()
	if b.Index > len(r.messages) || b.Revision > r.historyRevision || b.Prefix == "" || activeContextPrefixDigest(r.messages, b.Index) != b.Prefix || b.Index < r.activeContextStart {
		r.mu.Unlock()
		return false, fmt.Errorf("stale context boundary")
	}
	if r.lastContextResetID == rid {
		if b.Index != r.activeContextStart {
			r.mu.Unlock()
			return false, fmt.Errorf("reset id conflicts with context boundary")
		}
		r.mu.Unlock()
		return false, nil
	}
	oldStart, oldID, oldRevision := r.activeContextStart, r.lastContextResetID, r.historyRevision
	r.activeContextStart = b.Index
	r.lastContextResetID = rid
	r.historyRevision++
	r.mu.Unlock()
	if err := r.persist(ctx); err != nil {
		r.mu.Lock()
		r.activeContextStart, r.lastContextResetID, r.historyRevision = oldStart, oldID, oldRevision
		r.mu.Unlock()
		return false, err
	}
	return true, nil
}

func (r *runtime) activeMessagesSnapshot() []llm.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.activeContextStart
	if start < 0 || start > len(r.messages) {
		start = len(r.messages)
	}
	return append([]llm.Message(nil), r.messages[start:]...)
}
