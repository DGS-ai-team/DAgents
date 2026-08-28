// Package update 提供 Manage Release Hub 版本检查与安装包下载的共享逻辑。
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultChannel 为 Release Hub 默认渠道。
	DefaultChannel = "stable"

	TokenHeader   = "x-dagents-a2a-token"
	AgentIDHeader = "x-dagents-agent-id"
)

// Status 为 Manage /v1/releases/check 结果的 Client 视图。
type Status struct {
	CurrentVersion   string `json:"current_version"`
	LatestVersion    string `json:"latest_version"`
	UpgradeAvailable bool   `json:"upgrade_available"`
	ManageReachable  bool   `json:"manage_reachable"`
	// ManageEnabled distinguishes a disabled Manage integration from a
	// temporarily unreachable one. Desktop shells use this to decide whether
	// the update action should be offered and whether it can be retried.
	ManageEnabled        bool           `json:"manage_enabled"`
	UpdateEnabled        bool           `json:"update_enabled"`
	CheckIntervalSeconds int            `json:"check_interval_seconds,omitempty"`
	LastCheckedAt        string         `json:"last_checked_at,omitempty"`
	Channel              string         `json:"channel"`
	Platform             string         `json:"platform"`
	ReleaseNotes         string         `json:"release_notes,omitempty"`
	Message              string         `json:"message,omitempty"`
	ApplyCommand         string         `json:"apply_command"`
	Asset                map[string]any `json:"asset,omitempty"`
	Deprecated           bool           `json:"deprecated,omitempty"`
	Delegate             string         `json:"delegate,omitempty"`
	DesktopAPI           string         `json:"desktop_api,omitempty"`
}

// CheckRequest 控制一次 Manage 版本检查。
type CheckRequest struct {
	ManageURL      string
	CurrentVersion string
	Platform       string
	Channel        string
	AgentID        string
	NodeToken      string
	ApplyCommand   string
	Client         *http.Client
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

// CheckURL 构造 Manage GET /v1/releases/check URL。
func CheckURL(manageURL, current, platform, channel string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(manageURL), "/")
	if base == "" {
		return "", fmt.Errorf("manage.url is empty")
	}
	q := url.Values{}
	q.Set("current", current)
	q.Set("platform", platform)
	q.Set("channel", channel)
	return base + "/v1/releases/check?" + q.Encode(), nil
}

// NormalizeAssetURLs 将相对 download_url 补全为绝对 URL。
func NormalizeAssetURLs(manageBase string, asset map[string]any) map[string]any {
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

// Check 向 Manage 查询是否有新版本。
func Check(req CheckRequest) Status {
	channel := strings.TrimSpace(req.Channel)
	if channel == "" {
		channel = DefaultChannel
	}
	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		platform = ReleasePlatform()
	}
	current := strings.TrimSpace(req.CurrentVersion)
	applyCommand := strings.TrimSpace(req.ApplyCommand)
	if applyCommand == "" {
		applyCommand = "dagents update"
	}

	base := Status{
		CurrentVersion:  current,
		LatestVersion:   current,
		ManageReachable: false,
		LastCheckedAt:   time.Now().UTC().Format(time.RFC3339),
		Channel:         channel,
		Platform:        platform,
		ApplyCommand:    applyCommand,
		Message:         "无法连接 Manage，暂无法检查更新",
	}

	endpoint, err := CheckURL(req.ManageURL, current, platform, channel)
	if err != nil {
		base.Message = err.Error()
		return base
	}
	httpReq, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		base.Message = err.Error()
		return base
	}
	if id := strings.TrimSpace(req.AgentID); id != "" {
		httpReq.Header.Set(AgentIDHeader, id)
	}
	if token := strings.TrimSpace(req.NodeToken); token != "" {
		httpReq.Header.Set(TokenHeader, token)
	}

	client := req.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(httpReq)
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
		out.Asset = NormalizeAssetURLs(req.ManageURL, asset)
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
