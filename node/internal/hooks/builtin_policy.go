package hooks

import (
	"context"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// PolicyToolHook 将 policy.Engine 三档策略接入 tool.before_each。
type PolicyToolHook struct {
	mu     sync.RWMutex
	engine *policy.Engine
}

// NewPolicyToolHook 构造内置 policy Hook。
func NewPolicyToolHook(engine *policy.Engine) *PolicyToolHook {
	return &PolicyToolHook{engine: engine}
}

// SetEngine 热更新策略引擎（与 turn.Orchestrator.SetPolicy 同步）。
func (h *PolicyToolHook) SetEngine(engine *policy.Engine) {
	if engine == nil {
		engine = policy.NewDefaultEngine()
	}
	h.mu.Lock()
	h.engine = engine
	h.mu.Unlock()
}

// Name 返回 Hook 标识。
func (h *PolicyToolHook) Name() string { return "builtin.policy" }

// Phases 返回支持的 phase 列表。
func (h *PolicyToolHook) Phases() []Phase { return []Phase{PhaseToolBeforeEach} }

// Run 实现通用 Hook，委托 RunToolBeforeEach。
func (h *PolicyToolHook) Run(ctx context.Context, hc *Context, host Host) (Result, error) {
	return runToolBeforeEachHook(ctx, hc, host, h.Name(), h.RunToolBeforeEach)
}

// RunToolBeforeEach 解析 toolMode 与 ResolvedAction。
func (h *PolicyToolHook) RunToolBeforeEach(_ context.Context, in ToolBeforeEachInput, out *ToolBeforeEachResult) error {
	h.mu.RLock()
	engine := h.engine
	h.mu.RUnlock()
	if engine == nil {
		out.ToolMode = policy.ModeRule
		out.Action = policy.ActionRequireApproval
		return nil
	}
	out.ToolMode = engine.ToolApprovalMode(in.ToolName)
	out.Action = engine.DecideTool(in.ToolName, in.ToolArgs)
	return nil
}
