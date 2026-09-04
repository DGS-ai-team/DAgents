package hooks

import (
	"context"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// DuplicateToolCallHook 在 rule+auto 路径检测短窗口内重复 tool call。
type DuplicateToolCallHook struct {
	cfg DuplicateConfig
	log *ToolExecutionLog
	now func() time.Time
}

// NewDuplicateToolCallHook 构造重复检测 Hook。
func NewDuplicateToolCallHook(cfg DuplicateConfig) *DuplicateToolCallHook {
	return &DuplicateToolCallHook{
		cfg: cfg,
		now: time.Now,
	}
}

// SetLog 绑定 session 级执行记录（每 Orchestrator 一份）。
func (h *DuplicateToolCallHook) SetLog(log *ToolExecutionLog) {
	h.log = log
}

// SetNow 注入时钟（单测）。
func (h *DuplicateToolCallHook) SetNow(fn func() time.Time) {
	if fn != nil {
		h.now = fn
	}
}

// Name 返回 Hook 标识。
func (h *DuplicateToolCallHook) Name() string { return "builtin.duplicate_tool_call" }

// Phases 返回支持的 phase 列表。
func (h *DuplicateToolCallHook) Phases() []Phase { return []Phase{PhaseToolBeforeEach} }

// Run 实现通用 Hook，委托 RunToolBeforeEach。
func (h *DuplicateToolCallHook) Run(ctx context.Context, hc *Context, host Host) (Result, error) {
	return runToolBeforeEachHook(ctx, hc, host, h.Name(), h.RunToolBeforeEach)
}

// RunToolBeforeEach 在 rule+auto 时比对 fingerprint，命中则改为 duplicate 审批。
func (h *DuplicateToolCallHook) RunToolBeforeEach(_ context.Context, in ToolBeforeEachInput, out *ToolBeforeEachResult) error {
	if h == nil || !h.cfg.IsEnabled() {
		return nil
	}
	if out.ToolMode != policy.ModeRule || out.Action != policy.ActionAuto {
		return nil
	}
	if isAgentOwnedTrustTool(in.ToolName) {
		return nil
	}
	if h.log == nil {
		return nil
	}
	last, ok := h.log.LastRecord()
	if !ok {
		return nil
	}
	toolName := strings.ToLower(strings.TrimSpace(in.ToolName))
	if toolName != strings.ToLower(strings.TrimSpace(last.ToolName)) {
		return nil
	}
	fp := ToolArgsFingerprint(in.ToolName, in.RawArguments)
	if fp != last.ArgsFingerprint {
		return nil
	}
	now := h.now()
	if now.Sub(last.ExecutedAt) > h.cfg.windowDuration() {
		return nil
	}
	meta := BuildDuplicateMeta(last, fp, h.cfg.WindowSeconds, now)
	out.Action = policy.ActionRequireApproval
	out.ApprovalReason = ApprovalSubtypeDuplicateToolCall
	out.ApprovalSubtype = ApprovalSubtypeDuplicateToolCall
	out.DuplicateMeta = &meta
	return nil
}
