// Package desktop 调用 Shell localhost Desktop API（F-ND2 / F-X8）。
package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	"github.com/DGS-ai-team/DAgents/shared/update"
)

var desktopAPIBaseURL = defaultBaseURLConst

const defaultBaseURLConst = "http://127.0.0.1:18767"

// BaseURL 返回 Shell Desktop API 基址。
func BaseURL() string {
	return desktopAPIBaseURL
}

// GetUpdateStatus 调用 Shell GET /v1/desktop/update。
func GetUpdateStatus(ctx context.Context, client *http.Client) (*update.Status, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL()+"/v1/desktop/update", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("desktop update HTTP %d", resp.StatusCode)
	}
	var status update.Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

// ResolveAgentUpdate 先查 Node；若返回 delegate=shell 则改读 Shell Desktop API。
func ResolveAgentUpdate(ctx context.Context, node *nodeapi.Client, httpClient *http.Client) (*update.Status, error) {
	status, err := node.GetAgentUpdate(ctx)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(status.Delegate), "shell") {
		return agentStatusToShared(status), nil
	}
	shellStatus, shellErr := GetUpdateStatus(ctx, httpClient)
	if shellErr == nil && shellStatus != nil {
		return shellStatus, nil
	}
	return agentStatusToShared(status), nil
}

// ToAgentUpdateStatus 将共享 Status 转为 Node API 视图（TUI 等复用）。
func ToAgentUpdateStatus(status *update.Status) *nodeapi.AgentUpdateStatus {
	if status == nil {
		return nil
	}
	return &nodeapi.AgentUpdateStatus{
		CurrentVersion:   status.CurrentVersion,
		LatestVersion:    status.LatestVersion,
		UpgradeAvailable: status.UpgradeAvailable,
		ManageReachable:  status.ManageReachable,
		LastCheckedAt:    status.LastCheckedAt,
		Channel:          status.Channel,
		Platform:         status.Platform,
		ReleaseNotes:     status.ReleaseNotes,
		Message:          status.Message,
		ApplyCommand:     status.ApplyCommand,
		Asset:            status.Asset,
		Deprecated:       status.Deprecated,
		Delegate:         status.Delegate,
		DesktopAPI:       status.DesktopAPI,
	}
}

func agentStatusToShared(status *nodeapi.AgentUpdateStatus) *update.Status {
	if status == nil {
		return nil
	}
	return &update.Status{
		CurrentVersion:   status.CurrentVersion,
		LatestVersion:    status.LatestVersion,
		UpgradeAvailable: status.UpgradeAvailable,
		ManageReachable:  status.ManageReachable,
		LastCheckedAt:    status.LastCheckedAt,
		Channel:          status.Channel,
		Platform:         status.Platform,
		ReleaseNotes:     status.ReleaseNotes,
		Message:          status.Message,
		ApplyCommand:     status.ApplyCommand,
		Asset:            status.Asset,
		Deprecated:       status.Deprecated,
		Delegate:         status.Delegate,
		DesktopAPI:       status.DesktopAPI,
	}
}
