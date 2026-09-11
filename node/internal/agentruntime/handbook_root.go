package agentruntime

import (
	"path/filepath"
	"strings"
)

// HandbookRoot resolves the sole Agent-bound handbook directory for both
// runtime tools and the management API. A relative configured directory stays
// inside the Agent's private state root; empty configuration uses the default.
func HandbookRoot(runtimeRoot, agentID string, workspace WorkspaceConfig, cfg HandbookConfig) (string, error) {
	workspaceRoot, err := EnsureWorkspace(runtimeRoot, agentID, workspace)
	if err != nil {
		return "", err
	}
	stateRoot, err := EnsureWorkspaceState(workspaceRoot, agentID)
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(cfg.Directory)
	if dir == "" {
		return filepath.Join(stateRoot, "handbook"), nil
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(stateRoot, dir)
	}
	return filepath.Abs(filepath.Clean(dir))
}
