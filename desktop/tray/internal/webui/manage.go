//go:build windows

package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodectl"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type agentInfoPayload struct {
	NodeID           string `json:"node_id"`
	ManageEnabled    bool   `json:"manage_enabled"`
	ManageURL        string `json:"manage_url"`
	ManageRegistered bool   `json:"manage_registered"`
}

// ManageConsoleURL 从 Node /v1/agent/info 构造 Manage Console URL（附带 node_id）。
func ManageConsoleURL(ctx context.Context, cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is nil")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.Local.Endpoint), "/")
	if base == "" {
		return "", fmt.Errorf("local.endpoint is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/agent/info", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("agent/info status %d", resp.StatusCode)
	}
	var info agentInfoPayload
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if !info.ManageEnabled {
		return "", fmt.Errorf("本机 Node 未启用 Manage（设置 › 连接 Manage）")
	}
	manageBase := strings.TrimRight(strings.TrimSpace(info.ManageURL), "/")
	if manageBase == "" {
		return "", fmt.Errorf("manage URL 为空")
	}
	nodeID := strings.TrimSpace(info.NodeID)
	if nodeID == "" {
		return "", fmt.Errorf("node ID 为空")
	}
	_ = info.ManageRegistered
	return manageBase + "/console/?node_id=" + url.QueryEscape(nodeID), nil
}

// OpenManage ensure Node 后在系统浏览器打开 Manage Console（带 node_id）。
func OpenManage(ctx context.Context, layout *nodectl.Layout, cfg *config.Config) error {
	if layout == nil || cfg == nil {
		return fmt.Errorf("layout or config is nil")
	}
	if err := nodectl.Start(ctx, layout, cfg, 30*time.Second); err != nil {
		return err
	}
	target, err := ManageConsoleURL(ctx, cfg)
	if err != nil {
		return err
	}
	return OpenURL(target)
}
