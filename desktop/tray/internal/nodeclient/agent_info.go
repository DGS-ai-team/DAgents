package nodeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// AgentInfo 为 GET /v1/agent/info 的桌面 Shell 视图。
type AgentInfo struct {
	NodeID        string `json:"node_id"`
	ManageEnabled bool   `json:"manage_enabled"`
	ManageURL     string `json:"manage_url"`
}

// AgentInfo 查询 Node 的有效 Manage 配置，供托盘决定是否启用入口。
func (c *Client) AgentInfo(ctx context.Context) (AgentInfo, error) {
	var out AgentInfo
	if c == nil || c.base == "" {
		return out, fmt.Errorf("node client: empty base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/agent/info", nil)
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
		return out, fmt.Errorf("GET /v1/agent/info: status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}
