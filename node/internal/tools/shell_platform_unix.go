//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// applyShellProcAttr 为 bash 子进程创建独立进程组，便于 cancel 时整组终止。
func applyShellProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// signalKillProcessGroup 向 shell 进程组发送 SIGTERM；失败时由调用方再 Kill 单进程。
func signalKillProcessGroup(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}
