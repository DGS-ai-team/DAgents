package agentruntime

import (
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// BuildParams 为构造 per-agent Registry / TurnOptions 的输入。
type BuildParams struct {
	NodeCFG    *config.Config
	BaseTurn   session.TurnOptions
	AgentID    string
	Snapshot   Snapshot
	BashTimeout int
}

// Built 为 per-agent 运行时产物。
type Built struct {
	FSRoot      string
	TurnOptions session.TurnOptions
	Registry    *tools.Registry
	ToolGroups  []string
}

// Build 根据快照构造 effective FSRoot、收紧后的工具组与独立 Registry。
func Build(p BuildParams) (Built, error) {
	if p.NodeCFG == nil {
		return Built{}, fmt.Errorf("node config required")
	}
	fsRoot := EffectiveFSRoot(p.NodeCFG.FSRoot, p.AgentID, p.Snapshot)
	groups := EnabledToolGroups(p.Snapshot)
	if len(groups) == 0 {
		groups = p.NodeCFG.Tools.NormalizedBuiltinEnabledGroups()
	}
	groups = ApplySandboxToolConstraints(groups, p.Snapshot)

	timeout := p.BashTimeout
	if timeout <= 0 {
		timeout = 30
	}
	reg, err := tools.NewRegistry(fsRoot, timeout, p.NodeCFG.Tools.BashOutputEncoding, p.NodeCFG.Tools.FileEncoding)
	if err != nil {
		return Built{}, err
	}
	tc := config.ToolsConfig{EnabledGroups: groups}
	if err := reg.SetBuiltinEnabled(tc.NormalizedBuiltinEnabled()); err != nil {
		return Built{}, err
	}
	mm := p.NodeCFG.MultimodalEnabled()
	if v := MultimodalEnabledFromDefaults(p.Snapshot); v != nil {
		mm = *v
	}
	if p.Snapshot.Sandbox.Enabled && !p.Snapshot.Sandbox.AllowNetworkTools {
		// 沙箱默认不开 multimodal 视觉浏览器路径依赖；仍允许显式模板打开。
	}
	reg.SetMultimodalEnabled(mm)
	bc := tools.DefaultBashCompressConfig()
	if p.NodeCFG.Tools.BashCompress.Enabled != nil {
		bc.Enabled = *p.NodeCFG.Tools.BashCompress.Enabled
	}
	if p.NodeCFG.Tools.BashCompress.MaxOutputChars > 0 {
		bc.MaxOutputChars = p.NodeCFG.Tools.BashCompress.MaxOutputChars
	}
	if p.NodeCFG.Tools.BashCompress.MaxOutputCharsStderr > 0 {
		bc.MaxOutputCharsStderr = p.NodeCFG.Tools.BashCompress.MaxOutputCharsStderr
	}
	reg.SetBashCompress(bc)

	turnOpts := p.BaseTurn
	turnOpts.FSRoot = fsRoot
	turnOpts.ToolResult.FSRoot = fsRoot
	turnOpts.MultimodalEnabled = mm
	ApplyDefaultsToTurnOptions(&turnOpts, p.Snapshot)

	return Built{
		FSRoot:      fsRoot,
		TurnOptions: turnOpts,
		Registry:    reg,
		ToolGroups:  groups,
	}, nil
}
