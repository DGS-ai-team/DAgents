//go:build windows

package tools

import (
	"os/exec"
	"strconv"
)

// applyShellProcAttr Windows 无 POSIX 进程组，保持默认 SysProcAttr。
func applyShellProcAttr(cmd *exec.Cmd) {}

// signalKillProcessGroup Windows 无进程组 SIGTERM，由调用方 Kill 单进程。
func signalKillProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
}
