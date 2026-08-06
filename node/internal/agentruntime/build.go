package agentruntime

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
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

// Build 根据快照构造 effective FSRoot、工具组与独立 Registry。
func Build(p BuildParams) (Built, error) {
	if p.NodeCFG == nil {
		return Built{}, fmt.Errorf("node config required")
	}
	fsRoot := EffectiveFSRoot(p.NodeCFG.FSRoot, p.AgentID, p.Snapshot)
	groups := EnabledToolGroups(p.Snapshot)

	timeout := p.BashTimeout
	if timeout <= 0 {
		timeout = 30
	}
	reg, err := tools.NewRegistry(fsRoot, timeout, p.NodeCFG.Tools.BashOutputEncoding, p.NodeCFG.Tools.FileEncoding)
	if err != nil {
		return Built{}, err
	}
	if len(groups) == 0 {
		reg.SetBuiltinEnabledNone()
	} else {
		if err := config.ValidateBuiltinToolGroups(groups); err != nil {
			return Built{}, err
		}
		if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups(groups)); err != nil {
			return Built{}, err
		}
	}
	mm := EffectiveMultimodalEnabled(p.NodeCFG, p.Snapshot)
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

	// 技能能力仅由 Agent 快照中的工具组 skills 决定；未配置或空列表表示未启用。
	skillsOn := toolGroupSelected(groups, "skills")
	skillsCfg := SkillsFromDefaults(p.Snapshot)

	turnOpts := p.BaseTurn
	turnOpts.FSRoot = fsRoot
	turnOpts.ToolResult.FSRoot = fsRoot
	turnOpts.MultimodalEnabled = mm
	turnOpts.SkillsEnabled = skillsOn
	if skillsOn {
		turnOpts.SkillsRoot = p.NodeCFG.SkillsRoot()
		turnOpts.SkillsMaxInPrompt = p.NodeCFG.Skills.MaxInPrompt
	}
	ApplyDefaultsToTurnOptions(&turnOpts, p.Snapshot)

	if skillsCfg.VisibleRestrict {
		turnOpts.SkillsVisibleRestrict = true
		turnOpts.SkillsVisible = append([]string(nil), skillsCfg.Visible...)
	}
	if turnOpts.SkillsEnabled && strings.TrimSpace(turnOpts.SkillsRoot) != "" {
		catalog := skills.NewCatalog(turnOpts.SkillsRoot, true, turnOpts.SkillsMaxInPrompt)
		if turnOpts.SkillsVisibleRestrict {
			catalog.RestrictVisible(turnOpts.SkillsVisible)
		}
		reg.SetSkillsCatalog(catalog)
	}

	return Built{
		FSRoot:      fsRoot,
		TurnOptions: turnOpts,
		Registry:    reg,
		ToolGroups:  groups,
	}, nil
}

func toolGroupSelected(groups []string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, g := range groups {
		if strings.EqualFold(strings.TrimSpace(g), name) {
			return true
		}
	}
	return false
}
