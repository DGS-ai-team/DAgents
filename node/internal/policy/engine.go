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
		return ModeAlways
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
	default:
		if name == "bash_run" {
			if e.bashRequiresApproval(toolArgs) {
				return ActionRequireApproval
			}
			return ActionAuto
		}
		if name == "trigger_list" || name == "trigger_get" || name == "ask_user_information" {
			return ActionAuto
		}
		if name == "trigger_create" || name == "trigger_update" || name == "trigger_delete" || name == "trigger_fire" {
			return ActionRequireApproval
		}
		if name == "background_job_status" {
			return ActionAuto
		}
		return ActionRequireApproval
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

func (e *Engine) bashRequiresApproval(toolArgs map[string]any) bool {
	if toolArgs == nil {
		return true
	}
	rawCommand, _ := toolArgs["command"].(string)
	rawCommand = strings.TrimSpace(rawCommand)
	if rawCommand == "" {
		return true
	}
	var shellTypePtr *string
	if raw, ok := toolArgs["shell_type"].(string); ok {
		s := strings.TrimSpace(raw)
		shellTypePtr = &s
	}
	shellType, ok := ResolveShellType(shellTypePtr)
	if !ok {
		return true
	}
	roots, parsed := ParseCommandRoots(rawCommand, shellType)
	if !parsed {
		return true
	}
	for _, root := range roots {
		mode := e.shellCommandMode(shellType, root)
		if mode == ModeAlways || mode == ModeRule {
			return true
		}
	}
	return false
}

func defaultEngine() *Engine {
	return &Engine{
		toolModes: map[string]ApprovalMode{
			"read_file":             ModeNever,
			"search_file":           ModeNever,
			"ask_user_information":  ModeNever,
			"write_file":            ModeAlways,
			"search_replace":        ModeAlways,
			"bash_run":              ModeRule,
			"background_job_status": ModeNever,
			"background_job_cancel": ModeAlways,
			"trigger_list":          ModeNever,
			"trigger_get":           ModeNever,
			"trigger_create":        ModeAlways,
			"trigger_update":        ModeAlways,
			"trigger_delete":        ModeAlways,
			"trigger_fire":          ModeAlways,
		},
		shellModes: map[ShellType]map[string]ApprovalMode{},
	}
}
