//go:build !windows

package nodectl

import (
	"context"
	"fmt"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// Start and Stop keep the update package buildable on non-Windows hosts.
// The desktop tray itself is Windows-only; callers must use the platform
// package on supported systems instead of silently attempting process control.
func Start(_ context.Context, _ *Layout, _ *config.Config, _ time.Duration) error {
	return fmt.Errorf("node control is only supported on Windows")
}

func Stop(_ context.Context, _ *Layout, _ *config.Config) error {
	return fmt.Errorf("node control is only supported on Windows")
}

func Restart(ctx context.Context, layout *Layout, cfg *config.Config, waitReady time.Duration) error {
	if err := Stop(ctx, layout, cfg); err != nil {
		return err
	}
	return Start(ctx, layout, cfg, waitReady)
}
