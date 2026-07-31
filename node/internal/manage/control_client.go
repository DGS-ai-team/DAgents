package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// ControlClient 调用 Manage 控制面（D5：Placement create/peers 已下线；仍保留 DELETE）。
type ControlClient struct {
	cfg    *config.Config
	client *http.Client
}

func NewControlClient(cfg *config.Config) *ControlClient {
	return &ControlClient{
		cfg:    cfg,
		client: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *ControlClient) enabled() bool {
	return c != nil && c.cfg != nil && c.cfg.Manage.Enabled && strings.TrimSpace(c.cfg.Manage.URL) != ""
}

func (c *ControlClient) manageURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(c.cfg.Manage.URL), "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (c *ControlClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	if !c.enabled() {
		return fmt.Errorf("manage is not enabled")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.manageURL(path), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(agentIDHeader, c.cfg.NodeID)
	if token := strings.TrimSpace(c.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("manage control status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

type controlDeleteResult struct {
	OK          bool   `json:"ok"`
	AgentID     string `json:"agent_id"`
	HomeNodeID  string `json:"home_node_id"`
	HomeDeleted bool   `json:"home_deleted"`
}

func (c *ControlClient) DeleteOnHome(ctx context.Context, homeNodeID, agentID string) error {
	homeNodeID = strings.TrimSpace(homeNodeID)
	agentID = strings.TrimSpace(agentID)
	if homeNodeID == "" || agentID == "" {
		return fmt.Errorf("home_node_id and agent_id required")
	}
	var out controlDeleteResult
	path := "/v1/control/nodes/" + homeNodeID + "/agents/" + agentID
	return c.doJSON(ctx, http.MethodDelete, path, nil, &out)
}
