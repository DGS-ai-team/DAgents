package hooks

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// AgentOwnedFileAfterHook 在写工具成功执行后更新 session 信任表。
type AgentOwnedFileAfterHook struct {
	cfg   AgentOwnedFileConfig
	trust *AgentFileTrust
}

// NewAgentOwnedFileAfterHook 构造信任链 after_each Hook。
func NewAgentOwnedFileAfterHook(cfg AgentOwnedFileConfig) *AgentOwnedFileAfterHook {
	return &AgentOwnedFileAfterHook{cfg: AgentOwnedFileConfigOrDefault(cfg)}
}

// SetTrust 绑定 session 级信任表。
func (h *AgentOwnedFileAfterHook) SetTrust(trust *AgentFileTrust) {
	h.trust = trust
}

// SetPathStater 注入 FS Stat。
func (h *AgentOwnedFileAfterHook) SetPathStater(stater PathStater) {
	if h == nil {
		return
	}
	h.cfg.PathStater = stater
}

// Name 返回 Hook 标识。
func (h *AgentOwnedFileAfterHook) Name() string { return "builtin.agent_owned_file_after" }

// Phases 返回支持的 phase 列表。
func (h *AgentOwnedFileAfterHook) Phases() []Phase { return []Phase{PhaseToolAfterEach} }

// Run 实现通用 Hook，委托 RunToolAfterEach。
func (h *AgentOwnedFileAfterHook) Run(ctx context.Context, hc *Context) (Result, error) {
	return runToolAfterEachHook(ctx, hc, h.Name(), h.RunToolAfterEach)
}

// RunToolAfterEach 写成功后更新 Owned / mtime。
func (h *AgentOwnedFileAfterHook) RunToolAfterEach(_ context.Context, in ToolAfterEachInput, _ *ToolAfterEachOutput) error {
	if h == nil || !h.cfg.Enabled || h.trust == nil {
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(in.RawResult), "ERROR:") {
		return nil
	}
	toolName := strings.ToLower(strings.TrimSpace(in.ToolName))
	if !isAgentOwnedTrustTool(toolName) {
		return nil
	}
	args := in.ToolArgs
	if len(args) == 0 && strings.TrimSpace(in.RawArguments) != "" {
		_, cleaned := tools.ParseToolCallArguments(in.RawArguments)
		_ = json.Unmarshal([]byte(cleaned), &args)
	}
	pathKey := WriteToolRelPath(toolName, args)
	if pathKey == "" {
		return nil
	}
	stater := h.cfg.PathStater
	if stater == nil {
		return nil
	}
	exists, mtime, err := stater.StatRelPath(pathKey)
	if err != nil || !exists {
		return nil
	}
	switch toolName {
	case "write_file":
		if h.trust.ConsumePendingCreate(pathKey) {
			h.trust.MarkOwned(pathKey, mtime)
			return nil
		}
		if h.trust.IsOwned(pathKey) {
			h.trust.UpdateMtime(pathKey, mtime)
		}
	case "search_replace":
		if h.trust.IsOwned(pathKey) {
			h.trust.UpdateMtime(pathKey, mtime)
		}
	}
	return nil
}
