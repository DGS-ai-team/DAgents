// Package manage 实现 Node 向 Manage Release Hub 查询更新。
package manage

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
	"github.com/DGS-ai-team/DAgents/shared/update"
)

// UpdateStatus 为 Manage /v1/releases/check 结果的 Client 视图。
type UpdateStatus = update.Status

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
		channel = update.DefaultChannel
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
		channel = update.DefaultChannel
	}
	return update.Check(update.CheckRequest{
		ManageURL:      u.cfg.Manage.URL,
		CurrentVersion: version.Version,
		Platform:       ReleasePlatform(),
		Channel:        channel,
		AgentID:        u.cfg.AgentID,
		NodeToken:      u.cfg.Manage.NodeToken,
		ApplyCommand:   "dagents update",
		Client:         u.client,
	})
}

// ReleasePlatform 映射 runtime 到 Release Hub platform slug。
func ReleasePlatform() string {
	return update.ReleasePlatform()
}
