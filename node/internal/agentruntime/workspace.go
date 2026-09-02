package agentruntime

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Workspace modes are persisted in an Agent snapshot.  The mode is part of
// the placement of an Agent and is intentionally not editable after creation.
const (
	WorkspaceModePrivate      = "private"
	WorkspaceModeCustom       = "custom"
	WorkspaceModeLegacyShared = "legacy_shared"
	// workspaceStateDir is intentionally hidden from the model's normal file
	// listing. It contains Agent-owned sidecars which must live with the
	// selected workspace, while remaining isolated when Agents share it.
	workspaceStateDir = ".dagents"
)

// WorkspaceConfig describes the user-facing working directory of an Agent.
// Custom paths are stored as canonical absolute paths. Private and legacy
// shared paths are derived from the Node runtime and therefore keep Path empty.
type WorkspaceConfig struct {
	Mode string `json:"mode"`
	Path string `json:"path,omitempty"`
}

// NormalizeWorkspaceConfig validates a creation request and returns the
// immutable representation that should be written into the Agent snapshot.
// Empty mode means the new Agent-private workspace.
func NormalizeWorkspaceConfig(runtimeRoot, agentID string, requested WorkspaceConfig) (WorkspaceConfig, error) {
	mode := strings.ToLower(strings.TrimSpace(requested.Mode))
	if mode == "" {
		mode = WorkspaceModePrivate
	}
	switch mode {
	case WorkspaceModePrivate:
		if strings.TrimSpace(requested.Path) != "" {
			return WorkspaceConfig{}, fmt.Errorf("private workspace must not include a path")
		}
		return WorkspaceConfig{Mode: mode}, nil
	case WorkspaceModeCustom:
		clean, err := filepath.Abs(filepath.Clean(strings.TrimSpace(requested.Path)))
		if err != nil {
			return WorkspaceConfig{}, fmt.Errorf("normalize workspace path: %w", err)
		}
		if pathWithin(clean, nodeRuntimeRoot(runtimeRoot)) {
			return WorkspaceConfig{}, fmt.Errorf("custom workspace cannot be inside the Node runtime directory")
		}
		path, err := canonicalWorkspacePath(strings.TrimSpace(requested.Path))
		if err != nil {
			return WorkspaceConfig{}, err
		}
		if pathWithin(path, nodeRuntimeRoot(runtimeRoot)) {
			return WorkspaceConfig{}, fmt.Errorf("custom workspace cannot be inside the Node runtime directory")
		}
		if strings.TrimSpace(agentID) == "" {
			return WorkspaceConfig{}, fmt.Errorf("agent_id is required for workspace validation")
		}
		return WorkspaceConfig{Mode: mode, Path: path}, nil
	case WorkspaceModeLegacyShared:
		return WorkspaceConfig{}, fmt.Errorf("legacy_shared is reserved for migrated Agents")
	default:
		return WorkspaceConfig{}, fmt.Errorf("unsupported workspace mode %q", requested.Mode)
	}
}

// EffectiveWorkspaceRoot resolves a persisted workspace. A missing workspace
// field is deliberately treated as legacy_shared so existing Agents retain
// their old Node-global behavior after an upgrade.
func EffectiveWorkspaceRoot(runtimeRoot, agentID string, workspace WorkspaceConfig) (string, error) {
	root := strings.TrimSpace(runtimeRoot)
	if root == "" {
		return "", fmt.Errorf("node runtime root is required")
	}
	mode := strings.ToLower(strings.TrimSpace(workspace.Mode))
	switch mode {
	case "", WorkspaceModeLegacyShared:
		// Keep the legacy string semantics intact. Besides preserving existing
		// snapshots, this avoids changing relative test/runtime roots merely by
		// loading an old Agent.
		return root, nil
	case WorkspaceModePrivate:
		if strings.TrimSpace(agentID) == "" {
			return "", fmt.Errorf("agent_id is required for private workspace")
		}
		return filepath.Abs(filepath.Join(root, "agents", agentID, "workspace"))
	case WorkspaceModeCustom:
		if strings.TrimSpace(workspace.Path) == "" {
			return "", fmt.Errorf("custom workspace path is required")
		}
		path, err := canonicalWorkspacePath(workspace.Path)
		if err != nil {
			return "", err
		}
		if pathWithin(path, nodeRuntimeRoot(root)) {
			return "", fmt.Errorf("custom workspace cannot be inside the Node runtime directory")
		}
		return path, nil
	default:
		return "", fmt.Errorf("unsupported workspace mode %q", workspace.Mode)
	}
}

// EnsureWorkspace creates the effective directory for a private/custom Agent.
// It is safe to call during runtime reloads and after a Node restart.
func EnsureWorkspace(runtimeRoot, agentID string, workspace WorkspaceConfig) (string, error) {
	root, err := EffectiveWorkspaceRoot(runtimeRoot, agentID, workspace)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create workspace %q: %w", root, err)
	}
	// Do not create data/ here. It is retained only as a compatibility path for
	// existing workspaces; new workspaces should remain free of this directory.
	return root, nil
}

// WorkspaceStateRoot returns the private state namespace for one Agent inside
// its selected workspace. User files remain directly under workspaceRoot;
// Agent-owned sidecars use this namespace so two Agents can safely share the
// same workspace without sharing their history or generated artifacts.
func WorkspaceStateRoot(workspaceRoot, agentID string) (string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	id := strings.TrimSpace(agentID)
	if err := validateWorkspaceStateID(id); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("normalize workspace root: %w", err)
	}
	return filepath.Join(abs, workspaceStateDir, id), nil
}

// EnsureWorkspaceState creates the hidden per-Agent state namespace. The
// subdirectories are created eagerly so their ownership is explicit, while
// individual files remain lazily created by their owning components.
func EnsureWorkspaceState(workspaceRoot, agentID string) (string, error) {
	stateRoot, err := WorkspaceStateRoot(workspaceRoot, agentID)
	if err != nil {
		return "", err
	}
	for _, subdir := range []string{"history", "memory"} {
		if err := os.MkdirAll(filepath.Join(stateRoot, subdir), 0o755); err != nil {
			return "", fmt.Errorf("create Agent workspace state %q: %w", stateRoot, err)
		}
	}
	return stateRoot, nil
}

// WorkspaceHistoryRelativeRoot is the model-visible relative path prefix for
// raw message journals. It deliberately includes agent_id even though the
// state root is already per-Agent, making the ownership obvious in a shared
// workspace and in exported logs.
func WorkspaceHistoryRelativeRoot(agentID string) (string, error) {
	root, err := WorkspaceStateRelativeRoot(agentID)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(root, "history")), nil
}

// WorkspaceStateRelativeRoot returns the workspace-relative private state
// directory used as the base for Agent-owned sidecars.
func WorkspaceStateRelativeRoot(agentID string) (string, error) {
	id := strings.TrimSpace(agentID)
	if err := validateWorkspaceStateID(id); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(workspaceStateDir, id)), nil
}

func validateWorkspaceStateID(agentID string) error {
	if agentID == "" {
		return fmt.Errorf("agent_id is required for workspace state")
	}
	if strings.IndexByte(agentID, 0) >= 0 || agentID == "." || agentID == ".." ||
		strings.ContainsAny(agentID, `/\\`) {
		return fmt.Errorf("agent_id is invalid for workspace state")
	}
	return nil
}

func canonicalWorkspacePath(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("custom workspace path is required")
	}
	if strings.IndexByte(raw, 0) >= 0 {
		return "", fmt.Errorf("workspace path contains NUL")
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("custom workspace path must be absolute")
	}
	clean, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", fmt.Errorf("normalize workspace path: %w", err)
	}
	if info, statErr := os.Stat(clean); statErr == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("workspace path is not a directory")
		}
		resolved, evalErr := filepath.EvalSymlinks(clean)
		if evalErr != nil {
			return "", fmt.Errorf("resolve workspace path: %w", evalErr)
		}
		return filepath.Abs(resolved)
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("access workspace path: %w", statErr)
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return "", fmt.Errorf("create workspace path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	return filepath.Abs(resolved)
}

func nodeRuntimeRoot(raw string) string {
	root, err := filepath.Abs(strings.TrimSpace(raw))
	if err != nil {
		return filepath.Clean(raw)
	}
	if resolved, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		return resolved
	}
	return root
}

func pathWithin(path, root string) bool {
	pathAbs, pathErr := filepath.Abs(path)
	rootAbs, rootErr := filepath.Abs(root)
	if pathErr != nil || rootErr != nil {
		return false
	}
	pathAbs = filepath.Clean(pathAbs)
	rootAbs = filepath.Clean(rootAbs)
	if runtime.GOOS == "windows" {
		pathAbs = strings.ToLower(pathAbs)
		rootAbs = strings.ToLower(rootAbs)
	}
	return pathAbs == rootAbs || strings.HasPrefix(pathAbs, rootAbs+string(os.PathSeparator))
}
