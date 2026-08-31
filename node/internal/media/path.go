package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWorkspaceRoot 解析并确保 Agent workspace 目录存在。
func ResolveWorkspaceRoot(workspaceRoot string) (string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("workspace root empty and getwd failed: %w", err)
		}
		root = wd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create workspace root: %w", err)
	}
	return abs, nil
}

// ResolveUnderRoot 将相对路径解析为 workspace root 内的绝对路径。
func ResolveUnderRoot(workspaceRoot, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(raw) {
		abs, err := filepath.Abs(filepath.Clean(raw))
		if err != nil {
			return "", err
		}
		root := workspaceRoot
		if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
			return "", fmt.Errorf("path escapes workspace root: %s", raw)
		}
		return abs, nil
	}
	clean := filepath.Clean(raw)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace root: %s", raw)
	}
	full := filepath.Join(workspaceRoot, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, workspaceRoot+string(os.PathSeparator)) && abs != workspaceRoot {
		return "", fmt.Errorf("path escapes workspace root: %s", raw)
	}
	return abs, nil
}

// ResolveImagePath 解析图片路径；相对路径在 workspace 内，绝对路径可直接引用（可位于 workspace 外）。
func ResolveImagePath(workspaceRoot, raw string) (abs string, external bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, fmt.Errorf("path is required")
	}
	if filepath.IsAbs(raw) {
		abs, err = filepath.Abs(filepath.Clean(raw))
		if err != nil {
			return "", false, err
		}
		root, err := ResolveWorkspaceRoot(workspaceRoot)
		if err != nil {
			return "", false, err
		}
		return abs, !pathUnderRoot(abs, root), nil
	}
	abs, err = ResolveUnderRoot(workspaceRoot, raw)
	if err != nil {
		return "", false, err
	}
	return abs, false, nil
}

func pathUnderRoot(abs, root string) bool {
	abs, err := filepath.Abs(abs)
	if err != nil {
		return false
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return false
	}
	return abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator))
}
func MIMEForPath(relPath string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(relPath)))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

const MaxBytes = 10 << 20
