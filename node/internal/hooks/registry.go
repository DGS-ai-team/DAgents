package hooks

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// Registry 按 priority 顺序执行 tool.before_each Hook 链。
type Registry struct {
	policyHook    *PolicyToolHook
	duplicateHook *DuplicateToolCallHook
	beforeEach    []ToolBeforeEachHook
}

// NewRegistry 构造带内置 Policy + Duplicate Hook 的 Registry。
func NewRegistry(policyEngine *policy.Engine, dupCfg DuplicateConfig) *Registry {
	ph := NewPolicyToolHook(policyEngine)
	dh := NewDuplicateToolCallHook(dupCfg)
	return &Registry{
		policyHook:    ph,
		duplicateHook: dh,
		beforeEach:    []ToolBeforeEachHook{ph, dh},
	}
}

// SetPolicyEngine 热更新 policy（session policy API 写盘后调用）。
func (r *Registry) SetPolicyEngine(engine *policy.Engine) {
	if r == nil || r.policyHook == nil {
		return
	}
	r.policyHook.SetEngine(engine)
}

// SetToolExecutionLog 绑定 session 级 tool 执行记录（Duplicate Hook 使用）。
func (r *Registry) SetToolExecutionLog(log *ToolExecutionLog) {
	if r == nil || r.duplicateHook == nil {
		return
	}
	r.duplicateHook.SetLog(log)
}

// RunToolBeforeEach 执行 tool.before_each 链并返回合并决策。
func (r *Registry) RunToolBeforeEach(ctx context.Context, in ToolBeforeEachInput) ToolBeforeEachResult {
	out := ToolBeforeEachResult{
		Action:   policy.ActionRequireApproval,
		ToolMode: policy.ModeRule,
	}
	if r == nil {
		return out
	}
	for _, hook := range r.beforeEach {
		_ = hook.RunToolBeforeEach(ctx, in, &out)
	}
	return out
}
