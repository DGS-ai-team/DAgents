//go:build windows

package tools

import "os/exec"

// applyShellProcAttr Windows 无 POSIX 进程组，保持默认 SysProcAttr。
func applyShellProcAttr(cmd *exec.Cmd) {}

// signalKillProcessGroup Windows 无进程组 SIGTERM，由调用方 Kill 单进程。
func signalKillProcessGroup(cmd *exec.Cmd) {}
