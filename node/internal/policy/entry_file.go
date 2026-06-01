package policy

import (
	"fmt"
	"os"
	"strings"
)

// parseEntryFile 解析 `key=mode` 策略文件；忽略空行与 `#` 注释。
func parseEntryFile(path string, defaultMode ApprovalMode) (map[string]ApprovalMode, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ApprovalMode{}, nil
		}
		return nil, fmt.Errorf("read policy %q: %w", path, err)
	}
	mapping := make(map[string]ApprovalMode)
	for _, line := range strings.Split(string(raw), "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}
		if !strings.Contains(stripped, "=") {
			continue
		}
		keyRaw, modeRaw := stripped[:strings.Index(stripped, "=")], stripped[strings.Index(stripped, "=")+1:]
		key := strings.ToLower(strings.TrimSpace(keyRaw))
		if key == "" {
			continue
		}
		mapping[key] = normalizeMode(modeRaw, defaultMode)
	}
	return mapping, nil
}
