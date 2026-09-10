//go:build !windows

package platform

import (
	"fmt"
	"os/exec"
	"runtime"
)

func newDirectoryPicker() DirectoryPicker {
	if runtime.GOOS == "darwin" {
		return &commandDirectoryPicker{resolve: resolveMacDirectoryPicker}
	}
	return &commandDirectoryPicker{resolve: resolveLinuxDirectoryPicker}
}

func resolveMacDirectoryPicker() (string, []string, error) {
	command := "osascript"
	if _, err := exec.LookPath(command); err != nil {
		return "", nil, fmt.Errorf("%w: osascript not found", ErrDirectoryPickerUnavailable)
	}
	return command, []string{"-e", `POSIX path of (choose folder with prompt "选择工作目录")`}, nil
}

func resolveLinuxDirectoryPicker() (string, []string, error) {
	if command, err := exec.LookPath("zenity"); err == nil {
		return command, []string{"--file-selection", "--directory", "--title=选择工作目录"}, nil
	}
	if command, err := exec.LookPath("kdialog"); err == nil {
		return command, []string{"--getexistingdirectory", "", "--title", "选择工作目录"}, nil
	}
	if command, err := exec.LookPath("yad"); err == nil {
		return command, []string{"--file-selection", "--directory", "--title=选择工作目录"}, nil
	}
	return "", nil, fmt.Errorf("%w: install zenity, kdialog, or yad", ErrDirectoryPickerUnavailable)
}

func configureDirectoryPickerCommand(_ *exec.Cmd) {}
