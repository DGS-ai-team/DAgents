package tools

import "github.com/DGS-ai-team/DAgents/node/internal/sandbox"

// SetDockerSandbox 启用 Docker 沙箱：bash_run 经常驻容器 docker exec 执行。
// runner 为 nil 时恢复为本机进程执行。
func (r *Registry) SetDockerSandbox(runner *sandbox.DockerRunner) {
	if r == nil {
		return
	}
	r.dockerSandbox = runner
}

// DockerSandbox 返回已注入的 DockerRunner（可能为 nil）。
func (r *Registry) DockerSandbox() *sandbox.DockerRunner {
	if r == nil {
		return nil
	}
	return r.dockerSandbox
}
