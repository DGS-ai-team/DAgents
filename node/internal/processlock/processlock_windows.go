//go:build windows

package processlock

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
)

// AcquireNode 为 dagents-node 创建全局命名 Mutex；configPath 区分安装根/配置。
func AcquireNode(configPath string) (Release, error) {
	abs, err := filepath.Abs(strings.TrimSpace(configPath))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(strings.ToLower(abs)))
	name := fmt.Sprintf("Global\\DAgents-Node-%s", hex.EncodeToString(sum[:8]))
	return acquire(name)
}

func acquire(name string) (Release, error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	r0, _, callErr := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if r0 == 0 {
		return nil, callErr
	}
	handle := syscall.Handle(r0)
	if callErr == syscall.ERROR_ALREADY_EXISTS {
		_ = syscall.CloseHandle(handle)
		return nil, ErrAlreadyRunning
	}
	return func() { _ = syscall.CloseHandle(handle) }, nil
}
