package browser

import (
	"os"
	"path/filepath"
	"strings"
)

// SessionScreenshotPath 生成 workspace 下截图绝对路径；rel 返回相对 workspace 的路径。
func SessionScreenshotPath(workspaceRoot, outputDir, sessionKey, name string) (abs, rel string, err error) {
	root, err := filepath.Abs(strings.TrimSpace(workspaceRoot))
	if err != nil {
		return "", "", err
	}
	safeSession := sanitizePathSegment(sessionKey)
	safeName := sanitizePathSegment(name)
	if safeName == "" {
		safeName = "shot"
	}
	rel = filepath.ToSlash(filepath.Join(strings.Trim(outputDir, "/"), safeSession, safeName+".png"))
	abs = filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", "", err
	}
	return abs, rel, nil
}

func sanitizePathSegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "default"
	}
	return out
}
