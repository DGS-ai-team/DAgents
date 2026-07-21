package nodeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// AgentSummary 为 GET /v1/agents 列表项（托盘待办同步）。
type AgentSummary struct {
	AgentID          string `json:"agent_id"`
	DisplayName      string `json:"display_name,omitempty"`
	Active           bool   `json:"active,omitempty"`
	RunTurnPhase     string `json:"run_turn_phase,omitempty"`
	HasActiveTurn    bool   `json:"has_active_turn,omitempty"`
	NotifySeq        int    `json:"notify_seq,omitempty"`
	AckSeq           int    `json:"ack_seq,omitempty"`
	HasUnread        bool   `json:"has_unread,omitempty"`
	HasPendingHITL   bool   `json:"has_pending_hitl,omitempty"`
	PendingHITLItems int    `json:"pending_hitl_items,omitempty"`
}

type listAgentsResponse struct {
	Agents []AgentSummary `json:"agents"`
}

// ListAgents 拉取 Agent 列表（含未读 / HITL，供托盘待办同步）。
func (c *Client) ListAgents(ctx context.Context) ([]AgentSummary, error) {
	if c == nil || c.base == "" {
		return nil, fmt.Errorf("node client: empty base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/agents", nil)
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
		return nil, fmt.Errorf("GET /v1/agents: status %d", resp.StatusCode)
	}
	var out listAgentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Agents, nil
}

// SessionSummary 为历史别名；新代码请用 AgentSummary。
type SessionSummary = AgentSummary

// ListSessions 已废弃：转发到 ListAgents（兼容旧调用）。
func (c *Client) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	return c.ListAgents(ctx)
}
