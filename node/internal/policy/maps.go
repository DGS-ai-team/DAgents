package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Maps 为内存中的工具/shell 策略映射（可 JSON 序列化）。
type Maps struct {
	Tools map[string]ApprovalMode
	Shell map[ShellType]map[string]ApprovalMode
}

// NewEngineFromMaps 从映射构造 Engine（无文件依赖）。
func NewEngineFromMaps(m Maps) *Engine {
	tools := m.Tools
	if tools == nil {
		tools = map[string]ApprovalMode{}
	}
	shell := m.Shell
	if shell == nil {
		shell = map[ShellType]map[string]ApprovalMode{}
	}
	// 深拷贝，避免调用方改动影响 Engine。
	toolCopy := make(map[string]ApprovalMode, len(tools))
	for k, v := range tools {
		toolCopy[k] = v
	}
	shellCopy := make(map[ShellType]map[string]ApprovalMode, len(shell))
	for st, mapping := range shell {
		inner := make(map[string]ApprovalMode, len(mapping))
		for k, v := range mapping {
			inner[k] = v
		}
		shellCopy[st] = inner
	}
	return &Engine{toolModes: toolCopy, shellModes: shellCopy}
}

// ExportMaps 导出 Engine 当前映射副本。
func (e *Engine) ExportMaps() Maps {
	if e == nil {
		return Maps{
			Tools: map[string]ApprovalMode{},
			Shell: map[ShellType]map[string]ApprovalMode{},
		}
	}
	tools := make(map[string]ApprovalMode, len(e.toolModes))
	for k, v := range e.toolModes {
		tools[k] = v
	}
	shell := make(map[ShellType]map[string]ApprovalMode, len(e.shellModes))
	for st, mapping := range e.shellModes {
		inner := make(map[string]ApprovalMode, len(mapping))
		for k, v := range mapping {
			inner[k] = v
		}
		shell[st] = inner
	}
	return Maps{Tools: tools, Shell: shell}
}

// MapsToStringMaps 转为可 JSON 存库的 string map。
func MapsToStringMaps(m Maps) (tools map[string]string, shell map[string]map[string]string) {
	tools = make(map[string]string, len(m.Tools))
	for k, v := range m.Tools {
		tools[k] = string(v)
	}
	shell = make(map[string]map[string]string, len(m.Shell))
	for st, mapping := range m.Shell {
		inner := make(map[string]string, len(mapping))
		for k, v := range mapping {
			inner[k] = string(v)
		}
		shell[string(st)] = inner
	}
	return tools, shell
}

// StringMapsToMaps 从存库 string map 还原。
func StringMapsToMaps(tools map[string]string, shell map[string]map[string]string) Maps {
	out := Maps{
		Tools: map[string]ApprovalMode{},
		Shell: map[ShellType]map[string]ApprovalMode{},
	}
	for k, v := range tools {
		name := strings.ToLower(strings.TrimSpace(k))
		if name == "" {
			continue
		}
		out.Tools[name] = normalizeMode(v, ModeRule)
	}
	for stRaw, mapping := range shell {
		st, err := ParseShellTypeParam(stRaw)
		if err != nil {
			continue
		}
		inner := make(map[string]ApprovalMode, len(mapping))
		for cmd, mode := range mapping {
			c := normalizeShellCommand(cmd)
			if c == "" {
				c = strings.ToLower(strings.TrimSpace(cmd))
			}
			if c == "" {
				continue
			}
			inner[c] = normalizeMode(mode, ModeRule)
		}
		out.Shell[st] = inner
	}
	return out
}

// ApplyToolUpdatesToMaps 合并工具策略到 Maps（不写盘）。
func ApplyToolUpdatesToMaps(m Maps, updates []ToolUpdate) (Maps, error) {
	if m.Tools == nil {
		m.Tools = map[string]ApprovalMode{}
	}
	for _, upd := range updates {
		name := strings.ToLower(strings.TrimSpace(upd.Name))
		if name == "" {
			return m, fmt.Errorf("tool name is required")
		}
		mode, err := resolveApprovalMode(upd.Mode, upd.Decision)
		if err != nil {
			return m, err
		}
		if name == protectedToolDeny && mode == ModeDeny {
			return m, fmt.Errorf("%s cannot be set to deny", protectedToolDeny)
		}
		m.Tools[name] = mode
	}
	return m, nil
}

// ApplyShellPolicyChangesToMaps 更新/删除 shell 策略（不写盘）。
func ApplyShellPolicyChangesToMaps(m Maps, shellType ShellType, updates []ShellUpdate, deletes []string) (Maps, error) {
	if m.Shell == nil {
		m.Shell = map[ShellType]map[string]ApprovalMode{}
	}
	mapping := m.Shell[shellType]
	if mapping == nil {
		mapping = map[string]ApprovalMode{}
	}
	for _, upd := range updates {
		cmd := normalizeShellCommand(upd.Command)
		if cmd == "" {
			return m, fmt.Errorf("command is required")
		}
		mode, err := resolveApprovalMode(upd.Mode, upd.Decision)
		if err != nil {
			return m, err
		}
		mapping[cmd] = mode
	}
	for _, raw := range deletes {
		cmd := normalizeShellCommand(raw)
		if cmd == "" {
			return m, fmt.Errorf("command is required")
		}
		delete(mapping, cmd)
	}
	if len(updates) == 0 && len(deletes) == 0 {
		return m, fmt.Errorf("updates or deletes is required")
	}
	m.Shell[shellType] = mapping
	return m, nil
}

// LoadSeedMaps 从 packaging/runtime/policy 种子加载；种子不可用时返回空映射。
func LoadSeedMaps() Maps {
	seedDir := findSeedPolicyDir()
	if seedDir == "" {
		return Maps{
			Tools: map[string]ApprovalMode{},
			Shell: map[ShellType]map[string]ApprovalMode{
				ShellBash:       {},
				ShellCmd:        {},
				ShellPowerShell: {},
			},
		}
	}
	e, err := loadFromDir(seedDir)
	if err != nil {
		return Maps{
			Tools: map[string]ApprovalMode{},
			Shell: map[ShellType]map[string]ApprovalMode{
				ShellBash:       {},
				ShellCmd:        {},
				ShellPowerShell: {},
			},
		}
	}
	return e.ExportMaps()
}

// LoadMapsFromDir 从旧版 policy 目录加载（迁移用）。
func LoadMapsFromDir(policyDir string) (Maps, error) {
	policyDir = strings.TrimSpace(policyDir)
	if policyDir == "" {
		return Maps{}, fmt.Errorf("policy dir is required")
	}
	if _, err := os.Stat(filepath.Join(policyDir, "tool.approval.txt")); err != nil {
		if os.IsNotExist(err) {
			return Maps{}, err
		}
		return Maps{}, err
	}
	e, err := loadFromDir(policyDir)
	if err != nil {
		return Maps{}, err
	}
	return e.ExportMaps(), nil
}

// SortedToolNames 返回工具名排序列表（测试辅助）。
func SortedToolNames(m Maps) []string {
	names := make([]string, 0, len(m.Tools))
	for k := range m.Tools {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
