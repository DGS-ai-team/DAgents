package tools

import (
	"context"
	"fmt"
	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
)

func (r *Registry) acquireWorkspaceWrite(ctx context.Context, path string) (*workspacecoord.Lease, error) {
	if r == nil || r.workspaceCoordinator == nil {
		return nil, fmt.Errorf("workspace coordinator unavailable")
	}
	return r.workspaceCoordinator.TryAcquire(ctx, path, r.agentID)
}
