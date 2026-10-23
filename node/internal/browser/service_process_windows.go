//go:build windows

package browser

import (
	"os/exec"
	"syscall"
)

func configureBrowserCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
