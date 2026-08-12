package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const seedMergeMarker = "# --- seed merge: added missing tool modes ---"

// RetiredToolNames 为已拆除、不应再出现在策略 UI / 种子中的工具名（旧 A2A 等）。
var RetiredToolNames = []string{
	"agent_broadcast",
	"agent_send_message",
	"agent_peer_approve_tools",
	"agent_invoke",
	"agent_discover",
}

// PruneRetiredToolModes 从 dst 删除已退役工具条目；返回清理后的 map 与删除数量。
func PruneRetiredToolModes(dst map[string]string) (map[string]string, int) {
	if len(dst) == 0 {
		return dst, 0
	}
	retired := make(map[string]struct{}, len(RetiredToolNames))
	for _, name := range RetiredToolNames {
		retired[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	removed := 0
	for key := range dst {
		if _, ok := retired[strings.ToLower(strings.TrimSpace(key))]; ok {
			delete(dst, key)
			removed++
		}
	}
	return dst, removed
}

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

// MergeMissingSeedIntoRuntimePolicy 确保 `.runtime/policy` 存在，并把 packaging 种子缺项合并进 tool.approval.txt，
// 同时从运行时策略文件剔除已退役工具行。
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
	added, err := MergeMissingSeedToolsIntoFile(toolFile, seed.Tools)
	if err != nil {
		return added, err
	}
	if _, err := PruneRetiredToolsFromFile(toolFile); err != nil {
		return added, err
	}
	return added, nil
}

// PruneRetiredToolsFromFile 从 tool.approval.txt 删除已退役工具行（保留其它内容）；幂等。
func PruneRetiredToolsFromFile(toolFile string) (int, error) {
	toolFile = strings.TrimSpace(toolFile)
	if toolFile == "" {
		return 0, fmt.Errorf("tool policy file is required")
	}
	raw, err := os.ReadFile(toolFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	retired := make(map[string]struct{}, len(RetiredToolNames))
	for _, name := range RetiredToolNames {
		retired[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	lines := strings.Split(string(raw), "\n")
	out := make([]string, 0, len(lines))
	removed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			key := trimmed
			if i := strings.IndexByte(trimmed, '='); i >= 0 {
				key = strings.TrimSpace(trimmed[:i])
			}
			if _, ok := retired[strings.ToLower(key)]; ok {
				removed++
				continue
			}
		}
		out = append(out, line)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := backupPolicyFile(toolFile); err != nil {
		return 0, err
	}
	body := strings.Join(out, "\n")
	if err := os.WriteFile(toolFile, []byte(body), 0o644); err != nil {
		return 0, err
	}
	return removed, nil
}
