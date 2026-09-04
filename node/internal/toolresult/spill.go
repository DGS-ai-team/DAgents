package toolresult

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func spillPaths(cfg Config, sessionID, toolCallID string) (relPath, absPath string, err error) {
	sessionID = sanitizePathSegment(sessionID)
	toolCallID = sanitizePathSegment(toolCallID)
	if sessionID == "" {
		sessionID = "unknown-session"
	}
	if toolCallID == "" {
		toolCallID = "unknown-call"
	}
	subdir := spillSubdir
	if agentID := sanitizePathSegment(cfg.AgentID); agentID != "" {
		subdir = filepath.Join(subdir, agentID)
	}
	relPath = filepath.ToSlash(filepath.Join(subdir, sessionID, toolCallID+".txt"))
	absPath = filepath.Join(cfg.WorkspaceRoot, filepath.FromSlash(relPath))
	return relPath, absPath, nil
}

func sanitizePathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return "x"
	}
	return out
}

func writeSpillFile(absPath, content string) error {
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("toolresult spill mkdir: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("toolresult spill write: %w", err)
	}
	return nil
}
