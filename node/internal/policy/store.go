package policy

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

const protectedToolDeny = "ask_user_information"

// ToolPolicyEntry 为工具策略条目（API/快照视图）。
type ToolPolicyEntry struct {
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	Configured bool   `json:"configured"`
}

// ShellPolicyEntry 为 shell 命令策略条目。
type ShellPolicyEntry struct {
	Command    string `json:"command"`
	Mode       string `json:"mode"`
	Configured bool   `json:"configured"`
}

// PlatformInfo 描述 Node 运行环境与默认 shell。
type PlatformInfo struct {
	GOOS         string `json:"goos"`
	DefaultShell string `json:"default_shell"`
}

// Snapshot 为 GET policy 返回的结构化视图。
type Snapshot struct {
	PolicyDir string                        `json:"policy_dir,omitempty"`
	AgentID   string                        `json:"agent_id,omitempty"`
	Source    string                        `json:"source,omitempty"` // sqlite | file | memory
	Platform  PlatformInfo                  `json:"platform"`
	Tools     []ToolPolicyEntry             `json:"tools"`
	Shell     map[string][]ShellPolicyEntry `json:"shell"`
}

// LoadSnapshot 从 Engine 构造结构化快照；toolNames 为 registry 已知工具名。
func LoadSnapshot(policyDir string, engine *Engine, toolNames []string) (*Snapshot, error) {
	return LoadSnapshotForAgent("", policyDir, "file", engine, toolNames)
}

// LoadSnapshotForAgent 带 agent 作用域元数据的快照。
func LoadSnapshotForAgent(agentID, policyDir, source string, engine *Engine, toolNames []string) (*Snapshot, error) {
	policyDir = strings.TrimSpace(policyDir)
	if engine == nil {
		return nil, fmt.Errorf("policy engine is nil")
	}
	nameSet := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		n := strings.ToLower(strings.TrimSpace(name))
		if n != "" {
			nameSet[n] = struct{}{}
		}
	}
	for name := range engine.toolModes {
		nameSet[name] = struct{}{}
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	tools := make([]ToolPolicyEntry, 0, len(names))
	for _, name := range names {
		mode := engine.toolMode(name)
		tools = append(tools, ToolPolicyEntry{
			Name:       name,
			Mode:       string(mode),
			Configured: engine.ToolConfigured(name),
		})
	}

	shell := map[string][]ShellPolicyEntry{
		string(ShellBash):       shellEntriesFor(engine, ShellBash),
		string(ShellCmd):        shellEntriesFor(engine, ShellCmd),
		string(ShellPowerShell): shellEntriesFor(engine, ShellPowerShell),
	}
	defaultShell, _ := ResolveShellType(nil)
	return &Snapshot{
		PolicyDir: policyDir,
		AgentID:   strings.TrimSpace(agentID),
		Source:    strings.TrimSpace(source),
		Platform: PlatformInfo{
			GOOS:         defaultShellPlatformGOOS(),
			DefaultShell: string(defaultShell),
		},
		Tools: tools,
		Shell: shell,
	}, nil
}

func defaultShellPlatformGOOS() string {
	return runtime.GOOS
}

func shellEntriesFor(engine *Engine, shellType ShellType) []ShellPolicyEntry {
	mapping := engine.shellModes[shellType]
	if len(mapping) == 0 {
		return []ShellPolicyEntry{}
	}
	commands := make([]string, 0, len(mapping))
	for cmd := range mapping {
		commands = append(commands, cmd)
	}
	sort.Strings(commands)
	out := make([]ShellPolicyEntry, 0, len(commands))
	for _, cmd := range commands {
		mode := mapping[cmd]
		out = append(out, ShellPolicyEntry{
			Command:    cmd,
			Mode:       string(mode),
			Configured: true,
		})
	}
	return out
}

// ToolUpdate 为 PUT /v1/policy/tools 的单项更新。
type ToolUpdate struct {
	Name string       `json:"name"`
	Mode ApprovalMode `json:"mode"`
}

// ShellUpdate 为 PUT /v1/policy/shell/{type} 的单项更新。
type ShellUpdate struct {
	Command string       `json:"command"`
	Mode    ApprovalMode `json:"mode"`
}

func resolveApprovalMode(mode ApprovalMode) (ApprovalMode, error) {
	if strings.TrimSpace(string(mode)) == "" {
		return "", fmt.Errorf("mode is required")
	}
	parsed, err := ParseApprovalMode(string(mode))
	if err != nil {
		return "", err
	}
	return parsed, nil
}

func normalizeShellCommand(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	root, _ := ParseCommandRoots(raw, ShellBash)
	if len(root) > 0 {
		return root[0]
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(fields[0]))
}

// ParseShellTypeParam 解析 API path/query 中的 shell 类型。
func ParseShellTypeParam(raw string) (ShellType, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch ShellType(raw) {
	case ShellBash, ShellCmd, ShellPowerShell:
		return ShellType(raw), nil
	default:
		return "", fmt.Errorf("unsupported shell type: %q", raw)
	}
}
