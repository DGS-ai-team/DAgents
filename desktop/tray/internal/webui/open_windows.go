//go:build windows

package webui

import (
	"fmt"
	"os/exec"
	"strings"
)

// OpenURL 用系统默认浏览器打开 URL。
func OpenURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty url")
	}
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", raw).Start()
}
