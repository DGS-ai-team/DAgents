package manage

import (
	"log/slog"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// RegistrationCard 从 config agent 块组装注册 Manage 时上报的 card blob（语义为 Node 名片）。
func RegistrationCard(cfg *config.Config) map[string]any {
	if cfg == nil {
		return nil
	}
	name := cfg.NodeDisplayName()
	desc := cfg.AgentDescription()
	out := map[string]any{
		"name":        name,
		"description": desc,
	}
	if caps := cfg.RegistrationCapabilities(); len(caps) > 0 {
		out["capabilities"] = append([]string(nil), caps...)
	}
	out["defaultInputModes"] = []string{"text"}
	out["defaultOutputModes"] = []string{"text"}

	meta := map[string]any{}
	for k, v := range cfg.Agent.Metadata {
		meta[k] = v
	}
	if role := cfg.AgentRole(); role != "" {
		meta["role"] = role
	}
	if len(meta) > 0 {
		out["metadata"] = meta
	}
	return out
}

// LogA2AProfileWarnings 启动时校验 A2A 入站开关与 inbox 是否一致。
func LogA2AProfileWarnings(cfg *config.Config, logger *slog.Logger) {
	if cfg == nil || !cfg.Manage.Enabled {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	expose := cfg.ExposeToPeersEffective()
	inbox := cfg.ManageA2AEnabled()

	if expose && !inbox {
		logger.Warn("accept_inbound=true but inbox polling disabled; A2A tasks will not be processed",
			"accept_inbound", expose,
			"a2a_enabled", inbox,
		)
	}
	if inbox && !expose {
		logger.Warn("inbox polling enabled but accept_inbound=false; peers cannot discover/invoke this node",
			"accept_inbound", expose,
			"a2a_enabled", inbox,
		)
	}
}
