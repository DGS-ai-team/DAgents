package hooks

import (
	"strings"
)

// SkillHookNamePrefix 为 skill 级 Hook 注册名的前缀（skill/<skillName>/…）。
const SkillHookNamePrefix = "skill/"

// RemovePhaseHooksByPrefix 移除 Hook.Name() 以 prefix 开头的 phase hook（用于 skill 卸载同步）。
func (r *Registry) RemovePhaseHooksByPrefix(prefix string) {
	if r == nil || prefix == "" || len(r.phaseHooks) == 0 {
		return
	}
	out := r.phaseHooks[:0]
	for _, reg := range r.phaseHooks {
		if reg.hook == nil || !strings.HasPrefix(reg.hook.Name(), prefix) {
			out = append(out, reg)
		}
	}
	r.phaseHooks = out
}

// PhaseHookNames 返回某 phase 已注册 Hook 名称（按 priority 排序）。
func (r *Registry) PhaseHookNames(phase Phase) []string {
	matched := r.phaseHooksFor(phase)
	names := make([]string, 0, len(matched))
	for _, reg := range matched {
		if reg.hook != nil {
			names = append(names, reg.hook.Name())
		}
	}
	return names
}
