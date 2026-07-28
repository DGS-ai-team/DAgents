package api

import (
	"encoding/json"
	"net/http"
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
	RemoteEndpoint    *string `json:"remote_endpoint"`
	RemoteAPIKey      *string `json:"remote_api_key"`
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
	if patch.RemoteEndpoint != nil {
		out.RemoteEndpoint = strings.TrimSpace(*patch.RemoteEndpoint)
	}
	if patch.RemoteAPIKey != nil {
		out.RemoteAPIKey = strings.TrimSpace(*patch.RemoteAPIKey)
	}
	return normalizeSandbox(out)
}

func normalizeSandbox(s agentruntime.SandboxSpec) (agentruntime.SandboxSpec, error) {
	backend := strings.ToLower(strings.TrimSpace(s.Backend))
	if backend == "" {
		backend = "process"
	}
	switch backend {
	case "process", "docker", "remote":
		s.Backend = backend
	default:
		return s, errInvalidSandboxBackend
	}
	if strings.TrimSpace(s.WorkspaceSubdir) == "" {
		s.WorkspaceSubdir = "data"
	}
	// docker / remote 为隔离沙箱；process 表示宿主机（或历史「应用层约束」，仍可读旧快照）。
	if s.Enabled && (s.Backend == "docker" || s.Backend == "remote") {
		s.FSRootIsolation = true
	}
	if s.Enabled && s.Backend == "docker" {
		if strings.TrimSpace(s.Image) == "" {
			s.Image = "dagents-sandbox:latest"
		}
		if strings.TrimSpace(s.Network) == "" {
			s.Network = "none"
		}
	}
	if s.Enabled && s.Backend == "remote" {
		if strings.TrimSpace(s.RemoteEndpoint) == "" {
			return s, errRemoteEndpointRequired
		}
	}
	return s, nil
}

var (
	errInvalidSandboxBackend  = errString("sandbox.backend must be process|docker|remote")
	errRemoteEndpointRequired = errString("remote 沙箱需要 remote_endpoint")
	errRemoteNotImplemented   = errString("远程沙箱尚未实现，请改用本机 Docker 或关闭沙箱")
)

type errString string

func (e errString) Error() string { return string(e) }

func writeSandboxReadyError(w http.ResponseWriter, err error) {
	code := "docker_unavailable"
	if err != nil && strings.Contains(err.Error(), "远程沙箱") {
		code = "remote_unavailable"
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	writeAPIError(w, http.StatusBadRequest, code, msg, nil)
}

// requireDockerSandboxReady 在启用 docker 沙箱时校验本机 Docker CLI；remote 暂未实现。
func requireDockerSandboxReady(s agentruntime.SandboxSpec) error {
	if !s.Enabled {
		return nil
	}
	backend := strings.ToLower(strings.TrimSpace(s.Backend))
	if backend == "remote" {
		return errRemoteNotImplemented
	}
	if backend != "docker" {
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
		RemoteEndpoint:    tpl.Sandbox.RemoteEndpoint,
		RemoteAPIKey:      tpl.Sandbox.RemoteAPIKey,
	}
}

func sandboxToMap(s agentruntime.SandboxSpec) map[string]any {
	m := map[string]any{
		"enabled":             s.Enabled,
		"backend":             s.Backend,
		"workspace_subdir":    s.WorkspaceSubdir,
		"fs_root_isolation":   s.FSRootIsolation,
		"allow_bash":          s.AllowBash,
		"allow_network_tools": s.AllowNetworkTools,
		"image":               s.Image,
		"network":             s.Network,
		"memory":              s.Memory,
		"cpus":                s.CPUs,
	}
	if strings.TrimSpace(s.RemoteEndpoint) != "" {
		m["remote_endpoint"] = s.RemoteEndpoint
	}
	if strings.TrimSpace(s.RemoteAPIKey) != "" {
		m["remote_api_key"] = s.RemoteAPIKey
	}
	return m
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
