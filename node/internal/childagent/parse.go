package childagent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func parseCreateInput(argsJSON string, cfg Config) (CreateInput, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return CreateInput{}, fmt.Errorf("invalid json: %w", err)
	}
	task := strings.TrimSpace(fmt.Sprint(raw["task"]))
	if task == "" {
		return CreateInput{}, fmt.Errorf("task is required")
	}
	purpose := strings.TrimSpace(fmt.Sprint(raw["purpose"]))
	if purpose == "" {
		return CreateInput{}, fmt.Errorf("purpose is required")
	}
	ttl := cfg.DefaultTTLSeconds
	if v, ok := raw["ttl_seconds"].(float64); ok && int(v) > 0 {
		ttl = int(v)
	}
	if ttl < 60 {
		ttl = 60
	}
	if ttl > cfg.MaxTTLSeconds {
		ttl = cfg.MaxTTLSeconds
	}
	maxTurns := cfg.DefaultMaxTurns
	if v, ok := raw["max_turns"].(float64); ok && int(v) > 0 {
		maxTurns = int(v)
	}
	if maxTurns < 1 {
		maxTurns = 1
	}
	if maxTurns > cfg.MaxMaxTurns {
		maxTurns = cfg.MaxMaxTurns
	}
	wait, _ := raw["wait"].(bool)
	var allowed []string
	if arr, ok := raw["allowed_tools"].([]any); ok {
		for _, item := range arr {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" && s != "<nil>" {
				allowed = append(allowed, s)
			}
		}
	}
	return CreateInput{
		Task:         task,
		Purpose:      purpose,
		AllowedTools: allowed,
		TTLSeconds:   ttl,
		MaxTurns:     maxTurns,
		Wait: wait,
	}, nil
}

func resolveAllowedTools(requested []string) ([]string, error) {
	parentSet := make(map[string]struct{}, len(ParentDelegatableTools()))
	for _, n := range ParentDelegatableTools() {
		parentSet[n] = struct{}{}
	}
	pick := requested
	if len(pick) == 0 {
		pick = DefaultChildAllowedTools()
	}
	out := make([]string, 0, len(pick))
	for _, name := range pick {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if IsParentOnlyTool(name) {
			return nil, fmt.Errorf("tool %q cannot be delegated to child agent", name)
		}
		if _, ok := parentSet[name]; !ok {
			return nil, fmt.Errorf("tool %q is not delegatable", name)
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("allowed_tools resolved empty")
	}
	return out, nil
}

// WaitTimeout 返回 wait_temporary_agents 默认超时。
func (m *Manager) WaitTimeout(requested int) time.Duration {
	if requested <= 0 {
		return time.Duration(m.cfg.DefaultWaitTimeoutSeconds) * time.Second
	}
	return time.Duration(requested) * time.Second
}
