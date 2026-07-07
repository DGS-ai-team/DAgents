//go:build windows

package singleinstance

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

// AcquireShell 为 dagents-shell 创建全局命名 Mutex；configPath 用于区分多安装根。
func AcquireShell(configPath string) (Release, error) {
	name, err := mutexName("Shell", configPath)
	if err != nil {
		return nil, err
	}
	return acquire(name)
}

func mutexName(kind, configPath string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(configPath))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.ToLower(abs)))
	return fmt.Sprintf("Global\\DAgents-%s-%s", kind, hex.EncodeToString(sum[:8])), nil
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
