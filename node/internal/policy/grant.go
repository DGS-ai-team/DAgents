package policy

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
)

// Grant is a temporary, Agent-owned authorization for a bounded tool set.
// Revoked and expired grants never widen an explicit deny policy.
type Grant struct {
	ID        string     `json:"id"`
	Tools     []string   `json:"tools"`
	Workspace string     `json:"workspace"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func (g Grant) Active(now time.Time) bool {
	return g.ID != "" && g.RevokedAt == nil && !g.ExpiresAt.IsZero() && now.Before(g.ExpiresAt)
}

func (g Grant) Allows(tool string, args map[string]any, now time.Time) bool {
	if !g.Active(now) || !filepath.IsAbs(g.Workspace) {
		return false
	}
	ok := false
	for _, name := range g.Tools {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(tool)) {
			ok = true
			break
		}
	}
	if !ok || !grantFileTool(tool) {
		return false
	}
	raw, exists := args["path"].(string)
	if !exists || strings.TrimSpace(raw) == "" {
		return false
	}
	path, err := workspacecoord.Canonical(raw)
	if err != nil {
		return false
	}
	root, err := workspacecoord.Canonical(g.Workspace)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func grantFileTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read_file", "write_file", "search_replace":
		return true
	default:
		return false
	}
}

// GrantToolSupported reports whether a tool has a bounded file path contract
// suitable for temporary workspace authorization.
func GrantToolSupported(name string) bool { return grantFileTool(name) }
