//go:build windows

package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
// 未完成首配时强制打开控制台首页，确保浏览器加载首配页。
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
	if nodeProfileIncomplete(ctx, cfg.Local.Endpoint) {
		targetURL = ConsoleURL(cfg.Local.Endpoint)
	}
	return OpenURL(targetURL)
}

// OpenConsole ensure Node 后打开控制台首页（F-U1）。
func OpenConsole(ctx context.Context, layout *nodectl.Layout, cfg *config.Config) error {
	return EnsureNodeAndOpen(ctx, layout, cfg, "")
}

type bootstrapOnboarding struct {
	Onboarding struct {
		NodeProfileCompleted *bool `json:"node_profile_completed"`
	} `json:"onboarding"`
}

func nodeProfileIncomplete(ctx context.Context, endpoint string) bool {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if base == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/ui/bootstrap", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body bootstrapOnboarding
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	return body.Onboarding.NodeProfileCompleted != nil && !*body.Onboarding.NodeProfileCompleted
}
