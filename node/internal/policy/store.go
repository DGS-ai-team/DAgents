package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const protectedToolDeny = "ask_user_information"

// ToolPolicyEntry 为工具策略条目（API/快照视图）。
type ToolPolicyEntry struct {
	Name       string   `json:"name"`
	Mode       string   `json:"mode"`
	Decision   Decision `json:"decision,omitempty"`
	Configured bool     `json:"configured"`
}

// ShellPolicyEntry 为 shell 命令策略条目。
type ShellPolicyEntry struct {
	Command    string   `json:"command"`
	Mode       string   `json:"mode"`
	Decision   Decision `json:"decision,omitempty"`
	Configured bool     `json:"configured"`
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
			Decision:   ModeToDecision(mode),
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
			Decision:   ModeToDecision(mode),
			Configured: true,
		})
	}
	return out
}

// ToolUpdate 为 PUT /v1/policy/tools 的单项更新。
type ToolUpdate struct {
	Name     string       `json:"name"`
	Mode     ApprovalMode `json:"mode,omitempty"`
	Decision Decision     `json:"decision,omitempty"`
}

// ShellUpdate 为 PUT /v1/policy/shell/{type} 的单项更新。
type ShellUpdate struct {
	Command  string       `json:"command"`
	Mode     ApprovalMode `json:"mode,omitempty"`
	Decision Decision     `json:"decision,omitempty"`
}

func resolveApprovalMode(mode ApprovalMode, decision Decision) (ApprovalMode, error) {
	if strings.TrimSpace(string(mode)) != "" {
		parsed, err := ParseApprovalMode(string(mode))
		if err != nil {
			return "", err
		}
		return parsed, nil
	}
	if strings.TrimSpace(string(decision)) != "" {
		return DecisionToMode(decision)
	}
	return "", fmt.Errorf("mode or decision is required")
}

// ApplyToolUpdates 合并工具策略并原子写盘。
func ApplyToolUpdates(policyDir string, updates []ToolUpdate) error {
	toolFile := filepath.Join(policyDir, "tool.approval.txt")
	mapping, err := parseEntryFile(toolFile, ModeRule)
	if err != nil {
		return err
	}
	for _, upd := range updates {
		name := strings.ToLower(strings.TrimSpace(upd.Name))
		if name == "" {
			return fmt.Errorf("tool name is required")
		}
		mode, err := resolveApprovalMode(upd.Mode, upd.Decision)
		if err != nil {
			return err
		}
		if name == protectedToolDeny && mode == ModeDeny {
			return fmt.Errorf("%s cannot be set to deny", protectedToolDeny)
		}
		mapping[name] = mode
	}
	if err := backupPolicyFile(toolFile); err != nil {
		return err
	}
	return writeEntryFile(toolFile, mapping, toolFileHeader)
}

// ApplyShellUpdates 合并 shell 命令策略并原子写盘。
func ApplyShellUpdates(policyDir string, shellType ShellType, updates []ShellUpdate) error {
	return ApplyShellPolicyChanges(policyDir, shellType, updates, nil)
}

// ApplyShellPolicyChanges 更新或删除 shell 命令策略条目；未列出的命令运行时默认需审批。
func ApplyShellPolicyChanges(policyDir string, shellType ShellType, updates []ShellUpdate, deletes []string) error {
	shellFile, err := shellPolicyPath(policyDir, shellType)
	if err != nil {
		return err
	}
	mapping, err := parseEntryFile(shellFile, ModeRule)
	if err != nil {
		return err
	}
	for _, upd := range updates {
		cmd := normalizeShellCommand(upd.Command)
		if cmd == "" {
			return fmt.Errorf("command is required")
		}
		mode, err := resolveApprovalMode(upd.Mode, upd.Decision)
		if err != nil {
			return err
		}
		mapping[cmd] = mode
	}
	for _, raw := range deletes {
		cmd := normalizeShellCommand(raw)
		if cmd == "" {
			return fmt.Errorf("command is required")
		}
		delete(mapping, cmd)
	}
	if len(updates) == 0 && len(deletes) == 0 {
		return fmt.Errorf("updates or deletes is required")
	}
	if err := backupPolicyFile(shellFile); err != nil {
		return err
	}
	return writeEntryFile(shellFile, mapping, shellFileHeader(shellType))
}

func shellPolicyPath(policyDir string, shellType ShellType) (string, error) {
	switch shellType {
	case ShellBash:
		return filepath.Join(policyDir, "shell", "bash.approval.txt"), nil
	case ShellCmd:
		return filepath.Join(policyDir, "shell", "cmd.approval.txt"), nil
	case ShellPowerShell:
		return filepath.Join(policyDir, "shell", "powershell.approval.txt"), nil
	default:
		return "", fmt.Errorf("unsupported shell type: %q", shellType)
	}
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

const (
	toolFileHeader = "# tool approval policy: tool_name=always|never|rule|deny\n"
)

func shellFileHeader(shellType ShellType) string {
	return fmt.Sprintf("# %s shell approval policy: command=always|never|rule|deny\n", shellType)
}

func writeEntryFile(path string, mapping map[string]ApprovalMode, header string) error {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(header)
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(string(mapping[key]))
		b.WriteByte('\n')
	}
	return atomicWriteFile(path, []byte(b.String()))
}

func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create policy dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write policy temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename policy file: %w", err)
	}
	return nil
}

func backupPolicyFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read policy for backup: %w", err)
	}
	if err := os.WriteFile(path+".bak", raw, 0o644); err != nil {
		return fmt.Errorf("write policy backup: %w", err)
	}
	return nil
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
