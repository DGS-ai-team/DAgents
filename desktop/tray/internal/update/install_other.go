//go:build !windows

package update

import "fmt"

// installReleasePackage is kept as a compile-time counterpart to the
// Windows updater. The desktop update flow is intentionally Windows-only.
func installReleasePackage(_, _ string) (installTransaction, error) {
	return installTransaction{}, fmt.Errorf("release installation is only supported on Windows")
}
