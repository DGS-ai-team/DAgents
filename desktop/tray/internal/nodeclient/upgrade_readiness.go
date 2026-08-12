package nodeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// UpgradeReadiness 为 GET /v1/agent/upgrade-readiness（F-ND1）。
type UpgradeReadiness struct {
	Ready            bool     `json:"ready"`
	HasActiveTurn    bool     `json:"has_active_turn"`
	ActiveTurnCount  int      `json:"active_turn_count"`
	ActiveSessionIDs []string `json:"active_session_ids,omitempty"`
}

// UpgradeReadiness 查询 Node 是否可安全应用升级（Shell apply 前必调）。
func (c *Client) UpgradeReadiness(ctx context.Context) (UpgradeReadiness, error) {
	var out UpgradeReadiness
	if c == nil || c.base == "" {
		return out, fmt.Errorf("node client: empty base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/agent/upgrade-readiness", nil)
	if err != nil {
		return out, err
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("GET /v1/agent/upgrade-readiness: status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}
