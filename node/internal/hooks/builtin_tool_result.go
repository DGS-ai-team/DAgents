package hooks

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/toolresult"
)

// ToolResultPackageHook 在 tool.after_each 对配置内工具做落盘 + history 头尾摘要。
type ToolResultPackageHook struct {
	cfg ToolResultConfig
}

// NewToolResultPackageHook 构造 WS3 结果打包 Hook。
func NewToolResultPackageHook(cfg ToolResultConfig) *ToolResultPackageHook {
	return &ToolResultPackageHook{cfg: ToolResultConfigOrDefault(cfg)}
}

func (h *ToolResultPackageHook) Name() string { return "tool_result_package" }

func (h *ToolResultPackageHook) Phases() []Phase { return []Phase{PhaseToolAfterEach} }

// Run 实现通用 Hook，委托 RunToolAfterEach。
func (h *ToolResultPackageHook) Run(ctx context.Context, hc *Context, host Host) (Result, error) {
	return runToolAfterEachHook(ctx, hc, host, h.Name(), h.RunToolAfterEach)
}

func (h *ToolResultPackageHook) RunToolAfterEach(_ context.Context, in ToolAfterEachInput, out *ToolAfterEachOutput) error {
	if out.ForClient == "" {
		out.ForClient = in.RawResult
	}
	if out.ForHistory == "" {
		out.ForHistory = in.RawResult
	}
	if !h.cfg.Enabled {
		return nil
	}
	if toolresult.IsTerminalOutputTool(in.ToolName) {
		client, history, _ := toolresult.PackageTerminal(in.RawResult, toolresult.DefaultTerminalHistoryMaxBytes)
		out.ForClient = client
		out.ForHistory = history
		return nil
	}
	res, err := toolresult.Package(h.cfg.toToolresultConfig(), in.SessionID, in.ToolCallID, in.ToolName, in.RawResult)
	if err != nil {
		return err
	}
	out.ForClient = res.ForClient
	out.ForHistory = res.ForHistory
	out.SpillPath = res.SpillPath
	return nil
}
