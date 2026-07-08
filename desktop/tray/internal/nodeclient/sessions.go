package nodeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// SessionSummary 为 GET /v1/sessions 列表项。
type SessionSummary struct {
	SessionID        string `json:"session_id"`
	AgentID          string `json:"agent_id"`
	Active           bool   `json:"active"`
	RunTurnPhase     string `json:"run_turn_phase,omitempty"`
	HasActiveTurn    bool   `json:"has_active_turn,omitempty"`
	NotifySeq        int    `json:"notify_seq,omitempty"`
	AckSeq           int    `json:"ack_seq,omitempty"`
	HasUnread        bool   `json:"has_unread,omitempty"`
	HasPendingHITL   bool   `json:"has_pending_hitl,omitempty"`
	PendingHITLItems int    `json:"pending_hitl_items,omitempty"`
}

type listSessionsResponse struct {
	Sessions []SessionSummary `json:"sessions"`
}

// ListSessions 拉取 session 列表（F-E10 轮询兜底）。
func (c *Client) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	if c == nil || c.base == "" {
		return nil, fmt.Errorf("node client: empty base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/sessions", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v1/sessions: status %d", resp.StatusCode)
	}
	var out listSessionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}
