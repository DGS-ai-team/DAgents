// Package update 实现 Shell 向 Manage Release Hub 查询更新。
package update

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
	sharedupdate "github.com/DGS-ai-team/DAgents/shared/update"
)

// UpgradeCallback 在新版本可用时回调（每个 latest 版本仅一次，F-N9）。
type UpgradeCallback func(status sharedupdate.Status)

// Checker 周期性向 Manage 查询是否有新版本；不依赖 Node 在跑。
type Checker struct {
	cfg       *config.Config
	effective *config.Config
	home      string
	logger    *slog.Logger
	client    *http.Client

	onUpgrade UpgradeCallback

	mu               sync.RWMutex
	checkMu          sync.Mutex
	cfgMu            sync.RWMutex
	status           sharedupdate.Status
	lastUpgradeToast string
}

// NewChecker 构造 Shell Release 检查 sidecar。
func NewChecker(cfg *config.Config, installHome string, logger *slog.Logger) *Checker {
	if logger == nil {
		logger = slog.Default()
	}
	channel := sharedupdate.DefaultChannel
	if cfg != nil {
		channel = strings.TrimSpace(cfg.Manage.Update.Channel)
	}
	if channel == "" {
		channel = sharedupdate.DefaultChannel
	}
	current := ReadInstallVersion(installHome)
	status := sharedupdate.Status{
		CurrentVersion: current,
		LatestVersion:  current,
		Channel:        channel,
		Platform:       sharedupdate.ReleasePlatform(),
		ApplyCommand:   "dagents update",
		ManageEnabled:  cfg != nil && cfg.Manage.Enabled,
		UpdateEnabled:  cfg != nil && cfg.ManageUpdateEnabled(),
	}
	if cfg != nil && !cfg.ManageUpdateEnabled() {
		status.Message = "Manage 未启用，无法检查更新"
	}
	return &Checker{
		cfg:    cfg,
		home:   installHome,
		logger: logger,
		client: &http.Client{Timeout: 20 * time.Second},
		status: status,
	}
}

// ReadInstallVersion 读取安装根目录 VERSION 文件；缺失时返回 dev。
func ReadInstallVersion(installHome string) string {
	data, err := os.ReadFile(filepath.Join(installHome, "VERSION"))
	if err != nil {
		return "dev"
	}
	v := strings.TrimSpace(string(data))
	if v == "" {
		return "dev"
	}
	return v
}

// SetUpgradeCallback 注册新版本 Toast/托盘回调（F-N9）。
func (c *Checker) SetUpgradeCallback(fn UpgradeCallback) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.onUpgrade = fn
	c.mu.Unlock()
}

// Snapshot 返回最近一次检查结果副本。
func (c *Checker) Snapshot() sharedupdate.Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// CheckNow refreshes effective Node settings before performing an immediate
// check. It is used by the tray menu as the manual retry path.
func (c *Checker) CheckNow() sharedupdate.Status {
	if c == nil {
		return sharedupdate.Status{}
	}
	c.checkMu.Lock()
	defer c.checkMu.Unlock()
	c.refreshRuntimeConfig()
	c.checkOnce()
	return c.Snapshot()
}

// ConfigSnapshot returns the config used by the applier without exposing a
// mutable pointer while a runtime-settings refresh is in progress.
func (c *Checker) ConfigSnapshot() *config.Config {
	if c == nil {
		return nil
	}
	c.cfgMu.RLock()
	defer c.cfgMu.RUnlock()
	base := c.effective
	if base == nil {
		base = c.cfg
	}
	if base == nil {
		return nil
	}
	out := *base
	return &out
}

// Start 启动后台检查；ctx 取消时退出。
func (c *Checker) Start(ctx context.Context) {
	if c == nil || c.ConfigSnapshot() == nil {
		return
	}
	go c.run(ctx)
}

func (c *Checker) run(ctx context.Context) {
	status := c.CheckNow()
	interval := c.checkInterval(status)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status = c.CheckNow()
			next := c.checkInterval(status)
			if next != interval {
				ticker.Stop()
				interval = next
				ticker = time.NewTicker(interval)
			}
		}
	}
}

func (c *Checker) checkOnce() {
	status := c.fetchCheck()
	var cb UpgradeCallback
	var shouldNotify bool
	c.mu.Lock()
	prevToast := c.lastUpgradeToast
	c.status = status
	if status.ManageReachable && status.UpgradeAvailable {
		latest := strings.TrimSpace(status.LatestVersion)
		if latest != "" && latest != prevToast {
			c.lastUpgradeToast = latest
			shouldNotify = true
			cb = c.onUpgrade
		}
	} else if !status.UpgradeAvailable {
		c.lastUpgradeToast = ""
	}
	c.mu.Unlock()

	if status.ManageReachable && status.UpgradeAvailable {
		c.logger.Info(
			"release update available",
			"current", status.CurrentVersion,
			"latest", status.LatestVersion,
			"platform", status.Platform,
		)
	}
	if shouldNotify && cb != nil {
		cb(status)
	}
}

func (c *Checker) fetchCheck() sharedupdate.Status {
	cfg := c.ConfigSnapshot()
	if cfg == nil {
		return sharedupdate.Status{Message: "Shell 配置不可用"}
	}
	channel := strings.TrimSpace(cfg.Manage.Update.Channel)
	if channel == "" {
		channel = sharedupdate.DefaultChannel
	}
	status := sharedupdate.Check(sharedupdate.CheckRequest{
		ManageURL:      cfg.Manage.URL,
		CurrentVersion: ReadInstallVersion(c.home),
		Platform:       sharedupdate.ReleasePlatform(),
		Channel:        channel,
		AgentID:        cfg.NodeID,
		NodeToken:      cfg.Manage.NodeToken,
		ApplyCommand:   "dagents update",
		Client:         c.client,
	})
	status.ManageEnabled = cfg.Manage.Enabled
	status.UpdateEnabled = cfg.ManageUpdateEnabled()
	status.CheckIntervalSeconds = cfg.Manage.Update.CheckIntervalSeconds
	if !status.ManageEnabled || !status.UpdateEnabled {
		if !status.ManageEnabled {
			status.Message = "Manage 未启用，无法检查更新"
		} else {
			status.Message = "自动更新检查未启用"
		}
	}
	return status
}

type runtimeConfigPayload struct {
	NodeID                           string `json:"node_id"`
	ManageEnabled                    bool   `json:"manage_enabled"`
	ManageURL                        string `json:"manage_url"`
	ManageNodeToken                  string `json:"manage_node_token"`
	ManageUpdateEnabled              bool   `json:"manage_update_enabled"`
	ManageUpdateCheckIntervalSeconds int    `json:"manage_update_check_interval_seconds"`
	ManageUpdateChannel              string `json:"manage_update_channel"`
}

func (c *Checker) refreshRuntimeConfig() {
	cfg := c.ConfigSnapshot()
	if cfg == nil || strings.TrimSpace(cfg.Local.Endpoint) == "" {
		return
	}
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Local.Endpoint), "/") + "/v1/desktop/runtime-config"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var runtimeCfg runtimeConfigPayload
	if err := json.NewDecoder(resp.Body).Decode(&runtimeCfg); err != nil {
		return
	}
	cfg.Manage.Enabled = runtimeCfg.ManageEnabled
	cfg.Manage.URL = strings.TrimSpace(runtimeCfg.ManageURL)
	cfg.Manage.NodeToken = strings.TrimSpace(runtimeCfg.ManageNodeToken)
	cfg.Manage.Update.Enabled = boolPtr(runtimeCfg.ManageUpdateEnabled)
	cfg.Manage.Update.CheckIntervalSeconds = runtimeCfg.ManageUpdateCheckIntervalSeconds
	cfg.Manage.Update.Channel = strings.TrimSpace(runtimeCfg.ManageUpdateChannel)
	if strings.TrimSpace(runtimeCfg.NodeID) != "" {
		cfg.NodeID = strings.TrimSpace(runtimeCfg.NodeID)
	}
	c.cfgMu.Lock()
	c.effective = cfg
	c.cfgMu.Unlock()
}

func boolPtr(value bool) *bool {
	return &value
}

func (c *Checker) checkInterval(status sharedupdate.Status) time.Duration {
	if status.CheckIntervalSeconds > 0 {
		return time.Duration(status.CheckIntervalSeconds) * time.Second
	}
	if cfg := c.ConfigSnapshot(); cfg != nil {
		return cfg.ManageUpdateCheckInterval()
	}
	return 6 * time.Hour
}
