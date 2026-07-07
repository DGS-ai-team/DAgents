//go:build !windows

package processlock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// AcquireNode 在 Unix 上使用 flock 锁文件（同 config 单实例）。
func AcquireNode(configPath string) (Release, error) {
	abs, err := filepath.Abs(strings.TrimSpace(configPath))
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(dir, ".dagents-node.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %v", ErrAlreadyRunning, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
