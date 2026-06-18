package hitl

import (
	"fmt"
	"strings"
)

// 审批 resume 中 trigger 投递目标（硬编码枚举，非 LLM 指定）。
const (
	TriggerSessionSame         = "same_session"
	TriggerSessionNew          = "new_session"
	TriggerSessionLatestActive = "latest_active_session"
)

// ValidTriggerSessionTarget 校验审批选项枚举。
func ValidTriggerSessionTarget(raw string) bool {
	switch strings.TrimSpace(raw) {
	case TriggerSessionSame, TriggerSessionNew, TriggerSessionLatestActive:
		return true
	default:
		return false
	}
}

func parseTriggerSessionTargets(raw any, pending map[string]struct{}) (map[string]string, error) {
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(m))
	for key, val := range m {
		id := strings.TrimSpace(key)
		if id == "" {
			continue
		}
		if _, ok := pending[id]; !ok {
			return nil, fmt.Errorf("unknown trigger_session_targets id: %s", id)
		}
		target := strings.TrimSpace(fmt.Sprint(val))
		if !ValidTriggerSessionTarget(target) {
			return nil, fmt.Errorf("invalid trigger_session_target for %s: %q", id, target)
		}
		out[id] = target
	}
	return out, nil
}
