//go:build !windows

package browser

import "os/exec"

func configureBrowserCommand(*exec.Cmd) {}
