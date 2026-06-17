package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveFSRoot(fsRoot string) (string, error) {
	root := strings.TrimSpace(fsRoot)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("fs_root empty and getwd failed: %w", err)
		}
		root = wd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create fs_root: %w", err)
	}
	return abs, nil
}

func (r *Registry) resolvePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(raw) {
		abs, err := filepath.Abs(filepath.Clean(raw))
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	clean := filepath.Clean(raw)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes fs_root: %s", raw)
	}
	full := filepath.Join(r.fsRoot, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	root := r.fsRoot
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		return "", fmt.Errorf("path escapes fs_root: %s", raw)
	}
	return abs, nil
}
