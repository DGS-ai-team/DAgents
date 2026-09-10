//go:build windows

package platform

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"syscall"
	"unicode/utf16"
)

func newDirectoryPicker() DirectoryPicker {
	return &commandDirectoryPicker{resolve: resolveWindowsDirectoryPicker}
}

func resolveWindowsDirectoryPicker() (string, []string, error) {
	command := "powershell.exe"
	if _, err := exec.LookPath(command); err != nil {
		command = "pwsh.exe"
		if _, err := exec.LookPath(command); err != nil {
			return "", nil, fmt.Errorf("%w: powershell.exe or pwsh.exe not found", ErrDirectoryPickerUnavailable)
		}
	}
	return command, []string{
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-STA",
		"-EncodedCommand", encodeUTF16LE(windowsDirectoryPickerScript),
	}, nil
}

const windowsDirectoryPickerScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.Application]::EnableVisualStyles()
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = '选择工作目录'
$dialog.ShowNewFolderButton = $false
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
  [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
  [Console]::Out.Write($dialog.SelectedPath)
}`

func encodeUTF16LE(value string) string {
	runes := utf16.Encode([]rune(value))
	buf := make([]byte, len(runes)*2)
	for i, r := range runes {
		buf[i*2] = byte(r)
		buf[i*2+1] = byte(r >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func configureDirectoryPickerCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
