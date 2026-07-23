package agentruntime

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/sandbox"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// DefaultMaxToolLoops 为新建 Agent 未指定时的 defaults.llm.max_tool_loops。
const DefaultMaxToolLoops = 32

// BuildParams 为构造 per-agent Registry / TurnOptions 的输入。
type BuildParams struct {
	NodeCFG     *config.Config
	BaseTurn    session.TurnOptions
	AgentID     string
	Snapshot    Snapshot
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
// backend=docker 且 enabled 时注入 DockerRunner（bash_run 进容器）；调用方应先 RequireDocker。
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

	if err := attachDockerSandbox(reg, p.AgentID, fsRoot, p.Snapshot); err != nil {
		return Built{}, err
	}

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

func attachDockerSandbox(reg *tools.Registry, agentID, fsRoot string, snap Snapshot) error {
	if reg == nil || !snap.Sandbox.Enabled {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(snap.Sandbox.Backend), "docker") {
		return nil
	}
	runner, err := sandbox.NewDockerRunner(agentID, fsRoot, sandbox.Spec{
		Image:   snap.Sandbox.Image,
		Network: snap.Sandbox.Network,
		Memory:  snap.Sandbox.Memory,
		CPUs:    snap.Sandbox.CPUs,
	})
	if err != nil {
		return fmt.Errorf("docker sandbox: %w", err)
	}
	reg.SetDockerSandbox(runner)
	return nil
}
