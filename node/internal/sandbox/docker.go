// Package sandbox 提供 Agent 沙箱后端（process 应用层隔离 / docker 常驻容器隔离 bash）。
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultImage 为 backend=docker 且未指定 image 时的默认镜像（Alpine Linux 3.20 系）。
	DefaultImage = "dagents-sandbox:latest"
	// ContainerWorkspace 为容器内工作区挂载点。
	ContainerWorkspace = "/workspace"
	// DefaultIdleTimeout 为常驻容器无 bash 活动后的回收间隔。
	DefaultIdleTimeout = 15 * time.Minute
	// keepAliveCmd 保持容器进程存活（非一次性 run）。
	keepAliveCmd = "sleep infinity"
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

// lookPath / runDocker 可在单测中替换。
var lookPath = exec.LookPath

var runDocker = func(ctx context.Context, bin string, args ...string) (stdout string, stderr string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

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

// SetRunDockerForTest 仅供单测替换 docker 调用；返回 restore 函数。
func SetRunDockerForTest(fn func(ctx context.Context, bin string, args ...string) (string, string, error)) (restore func()) {
	old := runDocker
	runDocker = fn
	return func() { runDocker = old }
}

// Available 探测本机是否可用 docker CLI。
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

// DockerRunner 管理每个 Agent 一个常驻容器：Ensure 预创建，Command 走 docker exec，空闲/卸出时 Release。
type DockerRunner struct {
	AgentID      string
	Spec         Spec
	HostWorkDir  string
	Name         string
	IdleTimeout  time.Duration
	DockerBinary string

	mu       sync.Mutex
	running  bool
	lastUsed time.Time
}

// NewDockerRunner 构造 runner（尚未创建容器）；HostWorkDir 须存在，agentID 用于容器名。
func NewDockerRunner(agentID, hostWorkDir string, spec Spec) (*DockerRunner, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("docker sandbox: agent_id is required")
	}
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
		AgentID:     agentID,
		Spec:        NormalizeSpec(spec),
		HostWorkDir: abs,
		Name:        ContainerName(agentID),
		IdleTimeout: DefaultIdleTimeout,
	}, nil
}

func (r *DockerRunner) bin() string {
	if r != nil && strings.TrimSpace(r.DockerBinary) != "" {
		return strings.TrimSpace(r.DockerBinary)
	}
	return "docker"
}

func (r *DockerRunner) docker(ctx context.Context, args ...string) (string, string, error) {
	if runDocker == nil {
		return "", "", fmt.Errorf("docker sandbox: runDocker not configured")
	}
	return runDocker(ctx, r.bin(), args...)
}

// Ensure 预创建并启动常驻容器（已在跑则刷新 lastUsed）。
func (r *DockerRunner) Ensure(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("docker sandbox: runner is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ensureLocked(ctx)
}

func (r *DockerRunner) ensureLocked(ctx context.Context) error {
	if r.running {
		if r.containerRunning(ctx) {
			r.lastUsed = time.Now()
			return nil
		}
		r.running = false
	}
	// 清理可能残留的同名容器后重建。
	_, _, _ = r.docker(ctx, "rm", "-f", r.Name)

	args := r.buildCreateArgs()
	if _, stderr, err := r.docker(ctx, args...); err != nil {
		return fmt.Errorf("docker create %s: %w (%s)", r.Name, err, strings.TrimSpace(stderr))
	}
	if _, stderr, err := r.docker(ctx, "start", r.Name); err != nil {
		_, _, _ = r.docker(ctx, "rm", "-f", r.Name)
		return fmt.Errorf("docker start %s: %w (%s)", r.Name, err, strings.TrimSpace(stderr))
	}
	r.running = true
	r.lastUsed = time.Now()
	return nil
}

func (r *DockerRunner) buildCreateArgs() []string {
	spec := NormalizeSpec(r.Spec)
	args := []string{
		"create",
		"--name", r.Name,
		"--network", spec.Network,
		"-v", r.HostWorkDir + ":" + ContainerWorkspace + ":rw",
		"-w", ContainerWorkspace,
	}
	if spec.Memory != "" {
		args = append(args, "--memory", spec.Memory)
	}
	if spec.CPUs != "" {
		args = append(args, "--cpus", spec.CPUs)
	}
	if runtime.GOOS != "windows" {
		if uid := os.Getuid(); uid >= 0 {
			args = append(args, "--user", fmt.Sprintf("%d:%d", uid, os.Getgid()))
		}
	}
	args = append(args, spec.Image, "sleep", "infinity")
	return args
}

func (r *DockerRunner) containerRunning(ctx context.Context) bool {
	out, _, err := r.docker(ctx, "inspect", "-f", "{{.State.Running}}", r.Name)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

// BuildExecArgs 返回 `docker exec -i -w <cwd> <name> bash -lc <command>`（不含二进制名）。
func (r *DockerRunner) BuildExecArgs(hostCWD, command string) ([]string, error) {
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
	return []string{
		"exec", "-i",
		"-w", containerWD,
		r.Name,
		"bash", "-lc", command,
	}, nil
}

// Command 确保容器在跑后构造 docker exec 的 *exec.Cmd。
func (r *DockerRunner) Command(hostCWD, command string) (*exec.Cmd, error) {
	if r == nil {
		return nil, fmt.Errorf("docker sandbox: runner is nil")
	}
	r.mu.Lock()
	err := r.ensureLocked(context.Background())
	if err == nil {
		r.lastUsed = time.Now()
	}
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	args, err := r.BuildExecArgs(hostCWD, command)
	if err != nil {
		return nil, err
	}
	return exec.Command(r.bin(), args...), nil
}

// Release 停止并删除常驻容器。
func (r *DockerRunner) Release(ctx context.Context) {
	if r == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _, _ = r.docker(ctx, "rm", "-f", r.Name)
	r.running = false
}

// LastUsed 返回上次 Ensure/Command 时间（供空闲回收）。
func (r *DockerRunner) LastUsed() time.Time {
	if r == nil {
		return time.Time{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastUsed
}

// IsRunning 返回本进程认为容器仍应在跑（不保证 daemon 侧一致）。
func (r *DockerRunner) IsRunning() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// IdleExpired 判断是否超过空闲阈值（仅当曾启动过）。
func (r *DockerRunner) IdleExpired(now time.Time) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return false
	}
	timeout := r.IdleTimeout
	if timeout <= 0 {
		timeout = DefaultIdleTimeout
	}
	if r.lastUsed.IsZero() {
		return false
	}
	return now.Sub(r.lastUsed) >= timeout
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

// ContainerName 为 Agent 常驻容器命名。
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

// ReleaseAgent 按 agentID 强制删除同名容器（Pool 外兜底）。
func ReleaseAgent(agentID string) {
	name := ContainerName(agentID)
	if name == "" {
		return
	}
	if Available() != nil {
		return
	}
	_, _, _ = runDocker(context.Background(), "docker", "rm", "-f", name)
}
