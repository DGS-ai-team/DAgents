package hooks

import (
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// Registry 按 priority 顺序执行 tool.before_each / tool.after_each 与通用 RunPhase Hook 链。
type Registry struct {
	policyHook      *PolicyToolHook
	agentOwnedHook  *AgentOwnedFileHook
	agentOwnedAfter *AgentOwnedFileAfterHook
	duplicateHook   *DuplicateToolCallHook
	resultHook      *ToolResultPackageHook
	phaseHooks      []registeredPhaseHook
	journal         ExecutionJournal
}

// NewRegistry 构造带内置 Policy + AgentOwned + Duplicate + ToolResult Hook 的 Registry。
func NewRegistry(policyEngine *policy.Engine, runtimeCfg RuntimeConfig) *Registry {
	runtimeCfg = RuntimeConfigOrDefault(runtimeCfg)
	ph := NewPolicyToolHook(policyEngine)
	ah := NewAgentOwnedFileHook(runtimeCfg.AgentOwnedFile)
	aah := NewAgentOwnedFileAfterHook(runtimeCfg.AgentOwnedFile)
	dh := NewDuplicateToolCallHook(runtimeCfg.Duplicate)
	rh := NewToolResultPackageHook(runtimeCfg.ToolResult)
	r := &Registry{
		policyHook:      ph,
		agentOwnedHook:  ah,
		agentOwnedAfter: aah,
		duplicateHook:   dh,
		resultHook:      rh,
	}
	registerBuiltinToolBeforeEachHooks(r, ph, ah, dh)
	registerBuiltinToolAfterEachHooks(r, rh, aah)
	RegisterExternalEntries(r, runtimeCfg.External, runtimeCfg.ExternalDeps)
	return r
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

// SetAgentFileTrust 绑定 session 级 Agent 文件信任表。
func (r *Registry) SetAgentFileTrust(trust *AgentFileTrust) {
	if r == nil {
		return
	}
	if r.agentOwnedHook != nil {
		r.agentOwnedHook.SetTrust(trust)
	}
	if r.agentOwnedAfter != nil {
		r.agentOwnedAfter.SetTrust(trust)
	}
}

// SetPathStater 注入 FS Stat（Agent 文件信任链使用）。
func (r *Registry) SetPathStater(stater PathStater) {
	if r == nil {
		return
	}
	if r.agentOwnedHook != nil {
		r.agentOwnedHook.SetPathStater(stater)
	}
	if r.agentOwnedAfter != nil {
		r.agentOwnedAfter.SetPathStater(stater)
	}
}
