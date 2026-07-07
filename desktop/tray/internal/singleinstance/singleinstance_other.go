//go:build !windows

package singleinstance

// AcquireShell 非 Windows 平台为 no-op（Shell 仅 Windows）。
func AcquireShell(configPath string) (Release, error) {
	_ = configPath
	return func() {}, nil
}
