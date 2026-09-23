package browser

import (
	"fmt"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// NewDriver 创建唯一的 Playwright sidecar 驱动。
func NewDriver(cfg *config.Config) (Driver, error) {
	if cfg == nil || !cfg.BrowserEnabled() {
		return nil, fmt.Errorf("browser is disabled")
	}
	return NewRemoteDriver(cfg)
}
