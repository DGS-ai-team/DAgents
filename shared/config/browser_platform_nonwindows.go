//go:build !windows

package config

func browserPlatformSupported() bool { return true }

func browserPlatformReason() string { return "supported" }
