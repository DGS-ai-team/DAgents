package browser

import (
	"fmt"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// NewDriver 创建 remote 驱动（模式 A：dagents-browser + browser-use）。
func NewDriver(cfg *config.Config) (Driver, error) {
	if cfg == nil || !cfg.BrowserEnabled() {
		return nil, fmt.Errorf("browser is disabled")
	}
	return NewRemoteDriver(cfg)
}
