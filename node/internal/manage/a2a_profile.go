package manage

import (
	"log/slog"
	"strings"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// RegistrationCard 从 config.yaml agent 块组装注册 Manage 时上报的 card blob。
func RegistrationCard(cfg *config.Config) map[string]any {
	if cfg == nil {
		return nil
	}
	name := cfg.AgentDisplayName()
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
	if peer := cfg.CompliancePeer(); peer != "" {
		meta["compliance_peer"] = peer
	}
	if len(meta) > 0 {
		out["metadata"] = meta
	}
	return out
}

// LogA2AProfileWarnings 启动时校验 agent.role 与 inbox 配置是否一致。
func LogA2AProfileWarnings(cfg *config.Config, logger *slog.Logger) {
	if cfg == nil || !cfg.Manage.Enabled {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	role := cfg.AgentRole()
	effectiveExpose := cfg.ExposeToPeersEffective()
	inbox := cfg.ManageA2AEnabled()

	if cfg.Manage.A2A.Enabled != nil {
		roleDefaultInbox := config.ExposeToPeersForRole(role, nil)
		if inbox != roleDefaultInbox {
			logger.Warn("manage.a2a.enabled overrides role default inbox polling",
				"role", role,
				"a2a_enabled", inbox,
				"role_default", roleDefaultInbox,
			)
		}
	}

	if effectiveExpose && !strings.EqualFold(role, config.AgentRoleCompliance) {
		logger.Warn("agent exposed to A2A peers but agent.role is not compliance; inbox tasks may stall (no handler)",
			"role", role,
			"expose_to_peers", effectiveExpose,
			"inbox_poll", inbox,
		)
	}
	if strings.EqualFold(role, config.AgentRoleCompliance) && !inbox {
		logger.Warn("agent.role=compliance but inbox polling disabled; A2A tasks will not be processed",
			"role", role,
		)
	}
	if strings.EqualFold(role, config.AgentRoleCompliance) && !effectiveExpose {
		logger.Warn("agent.role=compliance but expose_to_peers is false; peers cannot invoke this agent",
			"role", role,
		)
	}
}
