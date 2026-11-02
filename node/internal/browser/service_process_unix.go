//go:build !windows

package browser

import (
	"os"
	"os/exec"
)

func startBrowserProcess(executable string, args []string, dir string, logFile *os.File) (stop func() error, wait func() error, err error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return cmd.Process.Kill, cmd.Wait, nil
}
