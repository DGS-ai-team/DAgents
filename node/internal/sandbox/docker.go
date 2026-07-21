// Package sandbox 提供 Agent 沙箱后端（process 应用层隔离 / docker 容器隔离 bash）。
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// DefaultImage 为 backend=docker 且未指定 image 时的默认镜像。
	DefaultImage = "dagents-sandbox:latest"
	// ContainerWorkspace 为容器内工作区挂载点。
	ContainerWorkspace = "/workspace"
)

// Spec 为 Docker 执行参数（来自 Agent SandboxSpec）。
type Spec struct {
	Image   string
	Network string
	Memory  string
	CPUs    string
}

// NormalizeSpec 填充 Docker 默认值。
func NormalizeSpec(s Spec) Spec {
	s.Image = strings.TrimSpace(s.Image)
	if s.Image == "" {
		s.Image = DefaultImage
	}
	s.Network = strings.TrimSpace(s.Network)
	if s.Network == "" {
		s.Network = "none"
	}
	s.Memory = strings.TrimSpace(s.Memory)
	s.CPUs = strings.TrimSpace(s.CPUs)
	return s
}

// lookPath 可在单测中替换。
var lookPath = exec.LookPath

// SetLookPathForTest 仅供单测替换 docker 探测；返回 restore 函数。
func SetLookPathForTest(fn func(file string) (string, error)) (restore func()) {
	old := lookPath
	if fn == nil {
		lookPath = exec.LookPath
	} else {
		lookPath = fn
	}
	return func() { lookPath = old }
}

// Available 探测本机是否可用 docker CLI（不强制 daemon 已响应，避免启动过慢）。
func Available() error {
	path, err := lookPath("docker")
	if err != nil {
		return fmt.Errorf("docker 不可用：未找到 docker CLI（backend=docker 需要 Docker）: %w", err)
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("docker 不可用：未找到 docker CLI")
	}
	return nil
}

// RequireDocker 在启用 docker 沙箱时校验环境。
func RequireDocker() error {
	return Available()
}

// DockerRunner 将 bash 命令封装为 docker run --rm。
type DockerRunner struct {
	Spec         Spec
	HostWorkDir  string // 宿主机 EffectiveFSRoot（bind-mount 源）
	DockerBinary string // 空则 "docker"
}

// NewDockerRunner 构造 runner；HostWorkDir 必须为绝对路径且存在。
func NewDockerRunner(hostWorkDir string, spec Spec) (*DockerRunner, error) {
	hostWorkDir = strings.TrimSpace(hostWorkDir)
	if hostWorkDir == "" {
		return nil, fmt.Errorf("docker sandbox: host workspace is required")
	}
	abs, err := filepath.Abs(hostWorkDir)
	if err != nil {
		return nil, fmt.Errorf("docker sandbox: resolve workspace: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("docker sandbox: workspace %q: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("docker sandbox: workspace is not a directory: %q", abs)
	}
	return &DockerRunner{
		Spec:        NormalizeSpec(spec),
		HostWorkDir: abs,
	}, nil
}

// BuildRunArgs 返回 `docker run ... image bash -lc <command>` 的完整 argv（含 docker 二进制名）。
// hostCWD 为宿主机 cwd（须落在 HostWorkDir 内）；映射为容器内 /workspace/... 。
func (r *DockerRunner) BuildRunArgs(hostCWD, command string) ([]string, error) {
	if r == nil {
		return nil, fmt.Errorf("docker sandbox: runner is nil")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("docker sandbox: command is empty")
	}
	containerWD, err := r.mapHostCWD(hostCWD)
	if err != nil {
		return nil, err
	}
	bin := strings.TrimSpace(r.DockerBinary)
	if bin == "" {
		bin = "docker"
	}
	spec := NormalizeSpec(r.Spec)
	args := []string{
		bin, "run", "--rm", "-i",
		"--network", spec.Network,
		"-v", r.HostWorkDir + ":" + ContainerWorkspace + ":rw",
		"-w", containerWD,
	}
	if spec.Memory != "" {
		args = append(args, "--memory", spec.Memory)
	}
	if spec.CPUs != "" {
		args = append(args, "--cpus", spec.CPUs)
	}
	// 使用宿主机 Node 进程 uid，保证 bind-mount 工作区可写。
	if runtime.GOOS != "windows" {
		if uid := os.Getuid(); uid >= 0 {
			args = append(args, "--user", fmt.Sprintf("%d:%d", uid, os.Getgid()))
		}
	}
	args = append(args, spec.Image, "bash", "-lc", command)
	return args, nil
}

// Command 构造可 Start 的 *exec.Cmd（Dir 留空：工作目录在容器 -w 内）。
func (r *DockerRunner) Command(hostCWD, command string) (*exec.Cmd, error) {
	argv, err := r.BuildRunArgs(hostCWD, command)
	if err != nil {
		return nil, err
	}
	return exec.Command(argv[0], argv[1:]...), nil
}

func (r *DockerRunner) mapHostCWD(hostCWD string) (string, error) {
	hostCWD = strings.TrimSpace(hostCWD)
	if hostCWD == "" {
		return ContainerWorkspace, nil
	}
	abs, err := filepath.Abs(hostCWD)
	if err != nil {
		return "", err
	}
	root := r.HostWorkDir
	sep := string(os.PathSeparator)
	if abs != root && !strings.HasPrefix(abs, root+sep) {
		return "", fmt.Errorf("docker sandbox: cwd escapes workspace: %s", hostCWD)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return ContainerWorkspace, nil
	}
	return ContainerWorkspace + "/" + rel, nil
}

// ContainerName 为可选长驻容器命名（MVP 使用 --rm 按次运行，删除 Agent 时可尝试 rm）。
func ContainerName(agentID string) string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return ""
	}
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, id)
	return "dagents-sbx-" + safe
}

// ReleaseAgent 尝试清理可能残留的命名容器（MVP 按次 --rm 时通常无操作）。
func ReleaseAgent(agentID string) {
	name := ContainerName(agentID)
	if name == "" {
		return
	}
	if Available() != nil {
		return
	}
	cmd := exec.Command("docker", "rm", "-f", name)
	_ = cmd.Run()
}
