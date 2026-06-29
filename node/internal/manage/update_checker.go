// Package manage 实现 Node 向 Manage Release Hub 查询更新。
package manage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

const defaultUpdateChannel = "stable"

// UpdateStatus 为 Manage /v1/releases/check 结果的 Client 视图。
type UpdateStatus struct {
	CurrentVersion   string         `json:"current_version"`
	LatestVersion    string         `json:"latest_version"`
	UpgradeAvailable bool           `json:"upgrade_available"`
	ManageReachable  bool           `json:"manage_reachable"`
	LastCheckedAt    string         `json:"last_checked_at,omitempty"`
	Channel          string         `json:"channel"`
	Platform         string         `json:"platform"`
	ReleaseNotes     string         `json:"release_notes,omitempty"`
	Message          string         `json:"message,omitempty"`
	ApplyCommand     string         `json:"apply_command"`
	Asset            map[string]any `json:"asset,omitempty"`
}

// UpdateChecker 周期性向 Manage 查询是否有新版本。
type UpdateChecker struct {
	cfg    *config.Config
	logger *slog.Logger
	client *http.Client

	mu     sync.RWMutex
	status UpdateStatus
}

// NewUpdateChecker 构造 Release 检查 sidecar。
func NewUpdateChecker(cfg *config.Config, logger *slog.Logger) *UpdateChecker {
	if logger == nil {
		logger = slog.Default()
	}
	channel := strings.TrimSpace(cfg.Manage.Update.Channel)
	if channel == "" {
		channel = defaultUpdateChannel
	}
	return &UpdateChecker{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 20 * time.Second},
		status: UpdateStatus{
			CurrentVersion: version.Version,
			LatestVersion:  version.Version,
			Channel:        channel,
			Platform:       ReleasePlatform(),
			ApplyCommand:   "dagents update",
		},
	}
}

// Snapshot 返回最近一次检查结果副本。
func (u *UpdateChecker) Snapshot() UpdateStatus {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.status
}

// Start 启动后台检查；ctx 取消时退出。
func (u *UpdateChecker) Start(ctx context.Context) {
	if u == nil || u.cfg == nil || !u.cfg.ManageUpdateEnabled() {
		return
	}
	go u.run(ctx)
}

func (u *UpdateChecker) run(ctx context.Context) {
	interval := u.cfg.ManageUpdateCheckInterval()
	u.checkOnce()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.checkOnce()
		}
	}
}

func (u *UpdateChecker) checkOnce() {
	status := u.fetchCheck()
	u.mu.Lock()
	u.status = status
	u.mu.Unlock()
	if status.ManageReachable && status.UpgradeAvailable {
		u.logger.Info(
			"release update available",
			"current", status.CurrentVersion,
			"latest", status.LatestVersion,
			"platform", status.Platform,
		)
	}
}

func (u *UpdateChecker) fetchCheck() UpdateStatus {
	channel := strings.TrimSpace(u.cfg.Manage.Update.Channel)
	if channel == "" {
		channel = defaultUpdateChannel
	}
	platform := ReleasePlatform()
	base := UpdateStatus{
		CurrentVersion:  version.Version,
		LatestVersion:   version.Version,
		ManageReachable: false,
		Channel:         channel,
		Platform:        platform,
		ApplyCommand:    "dagents update",
		Message:         "无法连接 Manage，暂无法检查更新",
	}
	endpoint, err := u.checkURL(version.Version, platform, channel)
	if err != nil {
		base.Message = err.Error()
		return base
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		base.Message = err.Error()
		return base
	}
	req.Header.Set(agentIDHeader, u.cfg.AgentID)
	if token := strings.TrimSpace(u.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return base
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return base
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return base
	}
	out := base
	out.ManageReachable = true
	out.LastCheckedAt = time.Now().UTC().Format(time.RFC3339)
	if latest, ok := raw["latest"].(string); ok && strings.TrimSpace(latest) != "" {
		out.LatestVersion = strings.TrimSpace(latest)
	}
	if notes, ok := raw["release_notes"].(string); ok {
		out.ReleaseNotes = notes
	}
	if upgrade, ok := raw["upgrade_available"].(bool); ok {
		out.UpgradeAvailable = upgrade
	}
	if asset, ok := raw["asset"].(map[string]any); ok && asset != nil {
		out.Asset = normalizeAssetURLs(u.cfg.Manage.URL, asset)
	}
	switch {
	case out.UpgradeAvailable:
		out.Message = fmt.Sprintf("新版本 %s 可用", out.LatestVersion)
	case out.LatestVersion == out.CurrentVersion:
		out.Message = "当前已是最新版本"
	default:
		out.Message = "暂无可用升级"
	}
	return out
}

func (u *UpdateChecker) checkURL(current, platform, channel string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(u.cfg.Manage.URL), "/")
	if base == "" {
		return "", fmt.Errorf("manage.url is empty")
	}
	q := url.Values{}
	q.Set("current", current)
	q.Set("platform", platform)
	q.Set("channel", channel)
	return base + "/v1/releases/check?" + q.Encode(), nil
}

func normalizeAssetURLs(manageBase string, asset map[string]any) map[string]any {
	out := make(map[string]any, len(asset))
	for k, v := range asset {
		out[k] = v
	}
	raw, _ := out["download_url"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return out
	}
	base := strings.TrimRight(strings.TrimSpace(manageBase), "/")
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	out["download_url"] = base + raw
	return out
}

// ReleasePlatform 映射 runtime 到 Release Hub platform slug。
func ReleasePlatform() string {
	switch runtime.GOOS {
	case "linux":
		if runtime.GOARCH == "arm64" {
			return "linux-arm64"
		}
		return "linux-amd64"
	case "windows":
		return "windows-amd64"
	default:
		return runtime.GOOS + "-" + runtime.GOARCH
	}
}
