// Package policy 加载 `.runtime/policy` 下的 txt 策略并判定工具/shell 执行策略。
package policy

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Action 为编排器使用的工具执行策略结果。
type Action string

const (
	ActionAuto            Action = "auto"
	ActionRequireApproval Action = "require_approval"
	ActionDeny            Action = "deny"
)

// Engine 按工具名与 shell root command 查表决策。
type Engine struct {
	toolModes  map[string]ApprovalMode
	shellModes map[ShellType]map[string]ApprovalMode
	policyDir  string
}

type yamlFileConfig struct {
	Default string            `yaml:"default"`
	Tools   map[string]string `yaml:"tools"`
}

// LoadFile 从 legacy YAML 加载策略（单测或显式 policy_file 覆盖时使用）。
func LoadFile(path string) (*Engine, error) {
	if strings.TrimSpace(path) == "" {
		return defaultEngine(), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultEngine(), nil
		}
		return nil, fmt.Errorf("read policy: %w", err)
	}
	var cfg yamlFileConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	toolModes := make(map[string]ApprovalMode)
	for name, mode := range cfg.Tools {
		toolModes[strings.ToLower(strings.TrimSpace(name))] = yamlActionToMode(mode)
	}
	return &Engine{toolModes: toolModes, shellModes: map[ShellType]map[string]ApprovalMode{}}, nil
}

func yamlActionToMode(raw string) ApprovalMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto", "never":
		return ModeNever
	case "deny":
		return ModeDeny
	default:
		return ModeRule
	}
}

// Decide 仅按工具名决策（无 bash 参数时保守处理 bash_run）。
func (e *Engine) Decide(toolName string) Action {
	return e.DecideTool(toolName, nil)
}

// DecideTool 对齐 Python `decide_tool_approval`：工具策略 + bash shell 子策略。
func (e *Engine) DecideTool(toolName string, toolArgs map[string]any) Action {
	if e == nil {
		return ActionRequireApproval
	}
	name := strings.ToLower(strings.TrimSpace(toolName))
	toolMode := e.toolMode(name)

	switch toolMode {
	case ModeAlways:
		return ActionRequireApproval
	case ModeNever:
		return ActionAuto
	case ModeDeny:
		return ActionDeny
	default:
		return e.decideToolRuleFallback(name, toolArgs)
	}
}

func (e *Engine) decideToolRuleFallback(toolName string, toolArgs map[string]any) Action {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "bash_run" {
		return e.bashDecideAction(toolArgs)
	}
	if name == "trigger_list" || name == "trigger_get" || name == "ask_user_information" {
		return ActionAuto
	}
	if name == "trigger_create" || name == "trigger_update" || name == "trigger_delete" {
		return ActionRequireApproval
	}
	if name == "background_job_status" {
		return ActionAuto
	}
	// tool-context-cost WS3/WS6：只读类 rule 工具首 call auto，短窗口重复由 DuplicateToolCallHook 拦截。
	if isRuleAutoReadTool(name) {
		return ActionAuto
	}
	return ActionRequireApproval
}

func isRuleAutoReadTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "read_file", "show_image", "glob_files", "grep_file", "grep_files", "agent_discover":
		return true
	default:
		return false
	}
}

func (e *Engine) toolMode(toolName string) ApprovalMode {
	if e == nil {
		return ModeRule
	}
	if mode, ok := e.toolModes[strings.ToLower(strings.TrimSpace(toolName))]; ok {
		return mode
	}
	return ModeRule
}

// ToolApprovalMode 返回工具在 policy 中的配置档位（always / never / rule / deny）。
func (e *Engine) ToolApprovalMode(toolName string) ApprovalMode {
	return e.toolMode(toolName)
}

func (e *Engine) shellCommandMode(shellType ShellType, root string) ApprovalMode {
	if e == nil {
		return ModeRule
	}
	mapping := e.shellModes[shellType]
	if mapping == nil {
		return ModeRule
	}
	if mode, ok := mapping[strings.ToLower(strings.TrimSpace(root))]; ok {
		return mode
	}
	return ModeRule
}

func (e *Engine) bashDecideAction(toolArgs map[string]any) Action {
	if toolArgs == nil {
		return ActionRequireApproval
	}
	rawCommand, _ := toolArgs["command"].(string)
	rawCommand = strings.TrimSpace(rawCommand)
	if rawCommand == "" {
		return ActionRequireApproval
	}
	var shellTypePtr *string
	if raw, ok := toolArgs["shell_type"].(string); ok {
		s := strings.TrimSpace(raw)
		shellTypePtr = &s
	}
	shellType, ok := ResolveShellType(shellTypePtr)
	if !ok {
		return ActionRequireApproval
	}
	roots, parsed := ParseCommandRoots(rawCommand, shellType)
	if !parsed {
		return ActionRequireApproval
	}
	hasRequire := false
	for _, root := range roots {
		switch e.shellCommandMode(shellType, root) {
		case ModeDeny:
			return ActionDeny
		case ModeAlways, ModeRule:
			hasRequire = true
		}
	}
	if hasRequire {
		return ActionRequireApproval
	}
	return ActionAuto
}

// PolicyDir 返回加载策略时的目录（可能为空）。
func (e *Engine) PolicyDir() string {
	if e == nil {
		return ""
	}
	return e.policyDir
}

// EffectiveToolDecision 返回工具在 catalog 中展示的有效三档策略。
func (e *Engine) EffectiveToolDecision(toolName string) Decision {
	if e == nil {
		return DecisionRequireApproval
	}
	action := e.DecideTool(toolName, nil)
	if toolName == "bash_run" && action == ActionRequireApproval {
		return DecisionRequireApproval
	}
	return actionToDecision(action)
}

// EffectiveShellDecision 返回 shell 命令的有效三档策略（未配置条目默认需审批）。
func (e *Engine) EffectiveShellDecision(shellType ShellType, command string) Decision {
	if e == nil {
		return DecisionRequireApproval
	}
	root := strings.ToLower(strings.TrimSpace(command))
	if root == "" {
		return DecisionRequireApproval
	}
	return ModeToDecision(e.shellCommandMode(shellType, root))
}

func actionToDecision(action Action) Decision {
	switch action {
	case ActionAuto:
		return DecisionAllowAuto
	case ActionDeny:
		return DecisionDeny
	default:
		return DecisionRequireApproval
	}
}

// ToolConfigured 是否在 tool.approval.txt 中有显式条目。
func (e *Engine) ToolConfigured(toolName string) bool {
	if e == nil {
		return false
	}
	_, ok := e.toolModes[strings.ToLower(strings.TrimSpace(toolName))]
	return ok
}

// ShellConfigured 是否在对应 shell 策略文件中有显式条目。
func (e *Engine) ShellConfigured(shellType ShellType, command string) bool {
	if e == nil {
		return false
	}
	mapping := e.shellModes[shellType]
	if mapping == nil {
		return false
	}
	_, ok := mapping[strings.ToLower(strings.TrimSpace(command))]
	return ok
}

func defaultEngine() *Engine {
	return &Engine{
		toolModes: map[string]ApprovalMode{
			"read_file":             ModeRule,
			"glob_files":            ModeRule,
			"grep_file":             ModeRule,
			"grep_files":            ModeRule,
			"search_file":           ModeNever,
			"ask_user_information":  ModeNever,
			"agent_discover":        ModeRule,
			"agent_invoke":          ModeRule,
			"write_file":            ModeRule,
			"search_replace":        ModeRule,
			"bash_run":              ModeRule,
			"background_job_status": ModeRule,
			"background_job_cancel": ModeAlways,
			"trigger_list":          ModeNever,
			"trigger_get":           ModeNever,
			"trigger_create":        ModeAlways,
			"trigger_update":        ModeAlways,
			"trigger_delete":        ModeAlways,
		},
		shellModes: map[ShellType]map[string]ApprovalMode{},
	}
}
