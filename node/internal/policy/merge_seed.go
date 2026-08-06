package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const seedMergeMarker = "# --- seed merge: added missing tool modes ---"

// MergeMissingToolModes 将 seed 中尚未出现在 dst 的工具模式补入 dst（不覆盖已有键）。
// dst 为 nil 时会新建 map；返回最终 map 与新增条目数。
func MergeMissingToolModes(dst map[string]string, seedTools map[string]ApprovalMode) (map[string]string, int) {
	if dst == nil {
		dst = map[string]string{}
	}
	added := 0
	for rawName, mode := range seedTools {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if name == "" {
			continue
		}
		if _, ok := dst[name]; ok {
			continue
		}
		dst[name] = string(mode)
		added++
	}
	return dst, added
}

// MergeMissingSeedToolsIntoFile 把种子中缺失的工具模式追加到已有 tool.approval.txt。
// 不覆盖已有键，保留原文件注释与用户改动；幂等。
func MergeMissingSeedToolsIntoFile(toolFile string, seedTools map[string]ApprovalMode) (int, error) {
	toolFile = strings.TrimSpace(toolFile)
	if toolFile == "" {
		return 0, fmt.Errorf("tool policy file is required")
	}
	if len(seedTools) == 0 {
		return 0, nil
	}
	existing, err := parseEntryFile(toolFile, ModeRule)
	if err != nil {
		return 0, err
	}
	var missing []string
	for rawName, mode := range seedTools {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if name == "" {
			continue
		}
		if _, ok := existing[name]; ok {
			continue
		}
		missing = append(missing, name+"="+string(mode))
	}
	if len(missing) == 0 {
		return 0, nil
	}
	sort.Strings(missing)
	if err := backupPolicyFile(toolFile); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(toolFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open policy for seed merge: %w", err)
	}
	defer f.Close()
	var b strings.Builder
	b.WriteByte('\n')
	b.WriteString(seedMergeMarker)
	b.WriteByte('\n')
	for _, line := range missing {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if _, err := f.WriteString(b.String()); err != nil {
		return 0, fmt.Errorf("append seed merge: %w", err)
	}
	return len(missing), nil
}

// MergeMissingSeedIntoRuntimePolicy 确保 `.runtime/policy` 存在，并把 packaging 种子缺项合并进 tool.approval.txt。
func MergeMissingSeedIntoRuntimePolicy(runtimeDir string) (int, error) {
	runtimeDir = strings.TrimSpace(runtimeDir)
	if runtimeDir == "" {
		runtimeDir = "./.runtime"
	}
	if err := ensureRuntimePolicyFiles(runtimeDir); err != nil {
		return 0, err
	}
	seed := LoadSeedMaps()
	toolFile := filepath.Join(runtimeDir, "policy", "tool.approval.txt")
	return MergeMissingSeedToolsIntoFile(toolFile, seed.Tools)
}
