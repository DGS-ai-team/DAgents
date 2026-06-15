package hooks

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// Registry 按 priority 顺序执行 tool.before_each / tool.after_each Hook 链。
type Registry struct {
	policyHook    *PolicyToolHook
	duplicateHook *DuplicateToolCallHook
	resultHook    *ToolResultPackageHook
	beforeEach    []ToolBeforeEachHook
	afterEach     []ToolAfterEachHook
}

// NewRegistry 构造带内置 Policy + Duplicate + ToolResult Hook 的 Registry。
func NewRegistry(policyEngine *policy.Engine, runtimeCfg RuntimeConfig) *Registry {
	runtimeCfg = RuntimeConfigOrDefault(runtimeCfg)
	ph := NewPolicyToolHook(policyEngine)
	dh := NewDuplicateToolCallHook(runtimeCfg.Duplicate)
	rh := NewToolResultPackageHook(runtimeCfg.ToolResult)
	return &Registry{
		policyHook:    ph,
		duplicateHook: dh,
		resultHook:    rh,
		beforeEach:    []ToolBeforeEachHook{ph, dh},
		afterEach:     []ToolAfterEachHook{rh},
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

// RunToolAfterEach 执行 tool.after_each 链，拆分 Client 与 history 正文。
func (r *Registry) RunToolAfterEach(ctx context.Context, in ToolAfterEachInput) ToolAfterEachOutput {
	out := ToolAfterEachOutput{
		ForClient:  in.RawResult,
		ForHistory: in.RawResult,
	}
	if r == nil {
		return out
	}
	for _, hook := range r.afterEach {
		_ = hook.RunToolAfterEach(ctx, in, &out)
	}
	return out
}
