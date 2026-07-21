package api

import (
	"encoding/json"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/agenttemplate"
	"github.com/DGS-ai-team/DAgents/node/internal/sandbox"
)

// sandboxPatch 为创建/更新请求中的沙箱字段（指针表示是否出现）。
type sandboxPatch struct {
	Enabled           *bool   `json:"enabled"`
	Backend           *string `json:"backend"`
	WorkspaceSubdir   *string `json:"workspace_subdir"`
	FSRootIsolation   *bool   `json:"fs_root_isolation"`
	AllowBash         *bool   `json:"allow_bash"`
	AllowNetworkTools *bool   `json:"allow_network_tools"`
	Image             *string `json:"image"`
	Network           *string `json:"network"`
	Memory            *string `json:"memory"`
	CPUs              *string `json:"cpus"`
}

func applySandboxPatch(base agentruntime.SandboxSpec, patch *sandboxPatch) (agentruntime.SandboxSpec, error) {
	out := base
	if strings.TrimSpace(out.Backend) == "" {
		out.Backend = "process"
	}
	if patch == nil {
		return normalizeSandbox(out)
	}
	if patch.Enabled != nil {
		out.Enabled = *patch.Enabled
	}
	if patch.Backend != nil && strings.TrimSpace(*patch.Backend) != "" {
		out.Backend = strings.TrimSpace(*patch.Backend)
	}
	if patch.WorkspaceSubdir != nil {
		out.WorkspaceSubdir = strings.TrimSpace(*patch.WorkspaceSubdir)
	}
	if patch.FSRootIsolation != nil {
		out.FSRootIsolation = *patch.FSRootIsolation
	}
	if patch.AllowBash != nil {
		out.AllowBash = *patch.AllowBash
	}
	if patch.AllowNetworkTools != nil {
		out.AllowNetworkTools = *patch.AllowNetworkTools
	}
	if patch.Image != nil {
		out.Image = strings.TrimSpace(*patch.Image)
	}
	if patch.Network != nil {
		out.Network = strings.TrimSpace(*patch.Network)
	}
	if patch.Memory != nil {
		out.Memory = strings.TrimSpace(*patch.Memory)
	}
	if patch.CPUs != nil {
		out.CPUs = strings.TrimSpace(*patch.CPUs)
	}
	return normalizeSandbox(out)
}

func normalizeSandbox(s agentruntime.SandboxSpec) (agentruntime.SandboxSpec, error) {
	backend := strings.ToLower(strings.TrimSpace(s.Backend))
	if backend == "" {
		backend = "process"
	}
	switch backend {
	case "process", "docker":
		s.Backend = backend
	default:
		return s, errInvalidSandboxBackend
	}
	if strings.TrimSpace(s.WorkspaceSubdir) == "" {
		s.WorkspaceSubdir = "data"
	}
	if s.Enabled && s.Backend == "docker" {
		// Docker 必须隔离工作区，否则容器挂载点语义不清。
		s.FSRootIsolation = true
		if strings.TrimSpace(s.Image) == "" {
			s.Image = "dagents-sandbox:latest"
		}
		if strings.TrimSpace(s.Network) == "" {
			s.Network = "none"
		}
	}
	return s, nil
}

var errInvalidSandboxBackend = errString("sandbox.backend must be process|docker")

type errString string

func (e errString) Error() string { return string(e) }

// requireDockerSandboxReady 在启用 docker 沙箱时校验本机 Docker CLI。
func requireDockerSandboxReady(s agentruntime.SandboxSpec) error {
	if !s.Enabled {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(s.Backend)) != "docker" {
		return nil
	}
	return sandbox.RequireDocker()
}

func sandboxFromTemplate(tpl *agenttemplate.Template) agentruntime.SandboxSpec {
	if tpl == nil {
		return agentruntime.SandboxSpec{Backend: "process", WorkspaceSubdir: "data", AllowBash: true, AllowNetworkTools: true}
	}
	return agentruntime.SandboxSpec{
		Enabled:           tpl.Sandbox.Enabled,
		Backend:           tpl.Sandbox.Backend,
		WorkspaceSubdir:   tpl.Sandbox.WorkspaceSubdir,
		FSRootIsolation:   tpl.Sandbox.FSRootIsolation,
		AllowBash:         tpl.Sandbox.AllowBash,
		AllowNetworkTools: tpl.Sandbox.AllowNetworkTools,
		Image:             tpl.Sandbox.Image,
		Network:           tpl.Sandbox.Network,
		Memory:            tpl.Sandbox.Memory,
		CPUs:              tpl.Sandbox.CPUs,
	}
}

func sandboxToMap(s agentruntime.SandboxSpec) map[string]any {
	return map[string]any{
		"enabled":              s.Enabled,
		"backend":              s.Backend,
		"workspace_subdir":     s.WorkspaceSubdir,
		"fs_root_isolation":    s.FSRootIsolation,
		"allow_bash":           s.AllowBash,
		"allow_network_tools":  s.AllowNetworkTools,
		"image":                s.Image,
		"network":              s.Network,
		"memory":               s.Memory,
		"cpus":                 s.CPUs,
	}
}

func marshalAgentSnapshot(templateID string, defaults map[string]any, sandbox agentruntime.SandboxSpec) (json.RawMessage, error) {
	if defaults == nil {
		defaults = map[string]any{}
	}
	snap := map[string]any{
		"template_id": templateID,
		"defaults":    defaults,
		"sandbox":     sandboxToMap(sandbox),
	}
	return json.Marshal(snap)
}
