//go:build windows

package config

import (
	"syscall"
	"unsafe"
)

const (
	windowsWorkstation = 1
	minimumWin11Build  = 22000
	minimumServer2019  = 17763
)

type rtlOSVersionInfoEx struct {
	OSVersionInfoSize uint32
	MajorVersion      uint32
	MinorVersion      uint32
	BuildNumber       uint32
	PlatformID        uint32
	CSDVersion        [128]uint16
	ServicePackMajor  uint16
	ServicePackMinor  uint16
	SuiteMask         uint16
	ProductType       byte
	_                 byte
}

func browserWindowsVersion() (major, build uint32, productType byte, ok bool) {
	dll := syscall.NewLazyDLL("ntdll.dll")
	proc := dll.NewProc("RtlGetVersion")
	info := rtlOSVersionInfoEx{OSVersionInfoSize: uint32(unsafe.Sizeof(rtlOSVersionInfoEx{}))}
	r1, _, _ := proc.Call(uintptr(unsafe.Pointer(&info)))
	if int32(r1) != 0 {
		return 0, 0, 0, false
	}
	return info.MajorVersion, info.BuildNumber, info.ProductType, true
}

func browserPlatformSupported() bool {
	major, build, productType, ok := browserWindowsVersion()
	if !ok || major < 10 {
		return false
	}
	if productType == windowsWorkstation {
		return build >= minimumWin11Build
	}
	return build >= minimumServer2019
}

func browserPlatformReason() string {
	major, build, productType, ok := browserWindowsVersion()
	if !ok {
		return "unable to detect Windows version"
	}
	if productType == windowsWorkstation && build < minimumWin11Build {
		return "Windows 11 or later is required"
	}
	if productType != windowsWorkstation && build < minimumServer2019 {
		return "Windows Server 2019 or later is required"
	}
	_ = major
	return "supported"
}
