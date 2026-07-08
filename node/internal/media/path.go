package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveFSRoot 解析并确保 fs_root 目录存在（与 tools.resolveFSRoot 语义一致）。
func ResolveFSRoot(fsRoot string) (string, error) {
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

// ResolveUnderRoot 将相对路径解析为 fs_root 内的绝对路径。
func ResolveUnderRoot(fsRoot, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(raw) {
		abs, err := filepath.Abs(filepath.Clean(raw))
		if err != nil {
			return "", err
		}
		root := fsRoot
		if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
			return "", fmt.Errorf("path escapes fs_root: %s", raw)
		}
		return abs, nil
	}
	clean := filepath.Clean(raw)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes fs_root: %s", raw)
	}
	full := filepath.Join(fsRoot, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, fsRoot+string(os.PathSeparator)) && abs != fsRoot {
		return "", fmt.Errorf("path escapes fs_root: %s", raw)
	}
	return abs, nil
}

// MIMEForPath 根据扩展名返回 image MIME，不支持则空字符串。
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
