//go:build !windows

package tools

import "os/exec"

func attachProcessTree(_ *exec.Cmd) (processTreeHandle, error) {
	// The shell already runs in its own process group on POSIX.
	return nil, nil
}
