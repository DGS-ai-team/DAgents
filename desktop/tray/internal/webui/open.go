//go:build windows

package webui

import (
	"context"
	"fmt"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodectl"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// EnsureNodeAndOpen 先 ensure Node 再打开 Web UI（F-U2）。
func EnsureNodeAndOpen(
	ctx context.Context,
	layout *nodectl.Layout,
	cfg *config.Config,
	sessionID string,
) error {
	return EnsureNodeAndOpenURL(ctx, layout, cfg, SessionURL(cfg.Local.Endpoint, sessionID))
}

// EnsureNodeAndOpenURL 先 ensure Node 再打开指定 URL。
func EnsureNodeAndOpenURL(
	ctx context.Context,
	layout *nodectl.Layout,
	cfg *config.Config,
	targetURL string,
) error {
	if layout == nil || cfg == nil {
		return fmt.Errorf("layout or config is nil")
	}
	if err := nodectl.Start(ctx, layout, cfg, 30*time.Second); err != nil {
		return err
	}
	return OpenURL(targetURL)
}

// OpenConsole ensure Node 后打开控制台首页（F-U1）。
func OpenConsole(ctx context.Context, layout *nodectl.Layout, cfg *config.Config) error {
	return EnsureNodeAndOpen(ctx, layout, cfg, "")
}
