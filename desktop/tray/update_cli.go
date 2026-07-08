//go:build windows

package main

import (
	"context"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/desktopapi"
)

func runUpdateCommand(args []string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	return desktopapi.RunUpdateCommand(ctx, args)
}
