package config

// BrowserPlatformSupported reports whether the current host is in the
// product's supported Playwright platform matrix. The implementation is split
// by build tag because Windows version detection requires the native API.
func BrowserPlatformSupported() bool { return browserPlatformSupported() }

// BrowserPlatformReason is suitable for health/status logs when the platform
// gate prevents registration of the Browser tool group.
func BrowserPlatformReason() string { return browserPlatformReason() }
