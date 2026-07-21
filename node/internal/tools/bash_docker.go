package tools

import "github.com/DGS-ai-team/DAgents/node/internal/sandbox"

// SetDockerSandbox 启用 Docker 沙箱：bash_run 经 docker run 执行，工作区 bind-mount 到 /workspace。
// runner 为 nil 时恢复为本机进程执行。
func (r *Registry) SetDockerSandbox(runner *sandbox.DockerRunner) {
	if r == nil {
		return
	}
	r.dockerSandbox = runner
}
