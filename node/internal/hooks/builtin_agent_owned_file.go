package hooks

import (
	"context"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// AgentOwnedFileHook 在 rule+require_approval 路径上，对已信任 Agent 自有文件降级为 auto。
type AgentOwnedFileHook struct {
	cfg   AgentOwnedFileConfig
	trust *AgentFileTrust
}

// NewAgentOwnedFileHook 构造信任链 before_each Hook。
func NewAgentOwnedFileHook(cfg AgentOwnedFileConfig) *AgentOwnedFileHook {
	return &AgentOwnedFileHook{cfg: AgentOwnedFileConfigOrDefault(cfg)}
}

// SetTrust 绑定 session 级信任表。
func (h *AgentOwnedFileHook) SetTrust(trust *AgentFileTrust) {
	h.trust = trust
}

// SetPathStater 注入 FS Stat（热更新 PathStater 时调用）。
func (h *AgentOwnedFileHook) SetPathStater(stater PathStater) {
	if h == nil {
		return
	}
	h.cfg.PathStater = stater
}

// Name 返回 Hook 标识。
func (h *AgentOwnedFileHook) Name() string { return "builtin.agent_owned_file" }

// Phases 返回支持的 phase 列表。
func (h *AgentOwnedFileHook) Phases() []Phase { return []Phase{PhaseToolBeforeEach} }

// Run 实现通用 Hook，委托 RunToolBeforeEach。
func (h *AgentOwnedFileHook) Run(ctx context.Context, hc *Context) (Result, error) {
	return runToolBeforeEachHook(ctx, hc, h.Name(), h.RunToolBeforeEach)
}

// RunToolBeforeEach 信任命中时将 rule+require_approval 降为 auto；always 档位不生效。
func (h *AgentOwnedFileHook) RunToolBeforeEach(_ context.Context, in ToolBeforeEachInput, out *ToolBeforeEachResult) error {
	if h == nil || !h.cfg.Enabled {
		return nil
	}
	if out.ToolMode != policy.ModeRule || out.Action != policy.ActionRequireApproval {
		return nil
	}
	toolName := strings.ToLower(strings.TrimSpace(in.ToolName))
	if !isAgentOwnedTrustTool(toolName) {
		return nil
	}
	pathKey := WriteToolRelPath(toolName, in.ToolArgs)
	if pathKey == "" {
		return nil
	}
	stater := h.cfg.PathStater
	if stater == nil {
		return nil
	}
	exists, mtime, err := stater.StatRelPath(pathKey)
	if err != nil {
		return nil
	}
	if toolName == "write_file" && !exists {
		if h.trust != nil {
			h.trust.SetPendingCreate(pathKey)
		}
		return nil
	}
	if !exists || h.trust == nil || !h.trust.IsOwned(pathKey) {
		return nil
	}
	last, ok := h.trust.LastWriteMtime(pathKey)
	if !ok || !mtime.Equal(last) {
		return nil
	}
	out.Action = policy.ActionAuto
	return nil
}

func isAgentOwnedTrustTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "write_file", "search_replace":
		return true
	default:
		return false
	}
}
