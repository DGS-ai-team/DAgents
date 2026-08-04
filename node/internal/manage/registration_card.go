package manage

import (
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
