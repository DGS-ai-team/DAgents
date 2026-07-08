// Package update 实现 Shell 向 Manage Release Hub 查询更新。
package update

import (
	"context"
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

// Checker 周期性向 Manage 查询是否有新版本；不依赖 Node 在跑。
type Checker struct {
	cfg    *config.Config
	home   string
	logger *slog.Logger
	client *http.Client

	mu     sync.RWMutex
	status sharedupdate.Status
}

// NewChecker 构造 Shell Release 检查 sidecar。
func NewChecker(cfg *config.Config, installHome string, logger *slog.Logger) *Checker {
	if logger == nil {
		logger = slog.Default()
	}
	channel := strings.TrimSpace(cfg.Manage.Update.Channel)
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

// Snapshot 返回最近一次检查结果副本。
func (c *Checker) Snapshot() sharedupdate.Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// Start 启动后台检查；ctx 取消时退出。
func (c *Checker) Start(ctx context.Context) {
	if c == nil || c.cfg == nil || !c.cfg.ManageUpdateEnabled() {
		return
	}
	go c.run(ctx)
}

func (c *Checker) run(ctx context.Context) {
	interval := c.cfg.ManageUpdateCheckInterval()
	c.checkOnce()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkOnce()
		}
	}
}

func (c *Checker) checkOnce() {
	status := c.fetchCheck()
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
	if status.ManageReachable && status.UpgradeAvailable {
		c.logger.Info(
			"release update available",
			"current", status.CurrentVersion,
			"latest", status.LatestVersion,
			"platform", status.Platform,
		)
	}
}

func (c *Checker) fetchCheck() sharedupdate.Status {
	channel := strings.TrimSpace(c.cfg.Manage.Update.Channel)
	if channel == "" {
		channel = sharedupdate.DefaultChannel
	}
	return sharedupdate.Check(sharedupdate.CheckRequest{
		ManageURL:      c.cfg.Manage.URL,
		CurrentVersion: ReadInstallVersion(c.home),
		Platform:       sharedupdate.ReleasePlatform(),
		Channel:        channel,
		AgentID:        c.cfg.AgentID,
		NodeToken:      c.cfg.Manage.NodeToken,
		ApplyCommand:   "dagents update",
		Client:         c.client,
	})
}
