//go:build windows

package nodectl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

const createNoWindow = 0x08000000

// Start 在后台启动 dagents-node，并等待 /health 就绪（最长 waitReady）。
func Start(ctx context.Context, layout *Layout, cfg *config.Config, waitReady time.Duration) error {
	if layout == nil || cfg == nil {
		return fmt.Errorf("layout or config is nil")
	}
	if _, err := os.Stat(layout.NodeExe); err != nil {
		return fmt.Errorf("node binary not found: %s", layout.NodeExe)
	}
	if nodectlIsRunning(ctx, cfg) {
		return nil
	}
	configArg, err := relativeConfigArg(layout.Home, layout.ConfigPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(layout.LogOut), 0o755); err != nil {
		return err
	}
	logOut, err := os.OpenFile(layout.LogOut, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logOut.Close()
	logErr, err := os.OpenFile(layout.LogErr, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logErr.Close()

	cmd := exec.CommandContext(ctx, layout.NodeExe, "-config", configArg)
	cmd.Dir = layout.Home
	cmd.Stdout = logOut
	cmd.Stderr = logErr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start node: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("node exited immediately: %w; see %s", err, layout.LogErr)
		}
		return fmt.Errorf("node exited immediately; see %s", layout.LogErr)
	case <-time.After(500 * time.Millisecond):
	}

	if err := os.WriteFile(layout.PidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, waitReady)
	defer cancel()
	return waitHealthy(waitCtx, cfg)
}

// Stop 停止 dagents-node 并清理 pid 文件。
func Stop(ctx context.Context, layout *Layout, cfg *config.Config) error {
	if layout == nil {
		return fmt.Errorf("layout is nil")
	}
	if !nodectlIsRunning(ctx, cfg) {
		_ = os.Remove(layout.PidFile)
		return nil
	}
	if pid, ok := readPID(layout.PidFile); ok {
		_ = exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T").Run()
		time.Sleep(300 * time.Millisecond)
		_ = exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !nodectlIsRunning(ctx, cfg) {
			_ = os.Remove(layout.PidFile)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("node still responds to /health after stop")
}

// Restart 先 Stop 再 Start。
func Restart(ctx context.Context, layout *Layout, cfg *config.Config, waitReady time.Duration) error {
	if err := Stop(ctx, layout, cfg); err != nil {
		return err
	}
	return Start(ctx, layout, cfg, waitReady)
}

func nodectlIsRunning(ctx context.Context, cfg *config.Config) bool {
	return IsRunning(ctx, cfg)
}

func waitHealthy(ctx context.Context, cfg *config.Config) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if IsRunning(ctx, cfg) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("node not ready: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func relativeConfigArg(home, configPath string) (string, error) {
	homeAbs, err := filepath.Abs(home)
	if err != nil {
		return "", err
	}
	cfgAbs, err := filepath.Abs(configPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(homeAbs, cfgAbs)
	if err != nil {
		return cfgAbs, nil
	}
	if rel == "." {
		return "config.yaml", nil
	}
	if !strings.HasPrefix(rel, "..") {
		return rel, nil
	}
	return cfgAbs, nil
}

func readPID(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}
