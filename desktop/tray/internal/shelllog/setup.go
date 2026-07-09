// Package shelllog 将 Shell 标准 log 输出落盘到 .runtime/logs/shell.log（F-I5）。
package shelllog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// Setup 追加写入 layout 下的 shell.log；返回 closer（可 nil）。
func Setup(home string) (io.Closer, error) {
	home = filepath.Clean(home)
	if home == "" {
		return nil, fmt.Errorf("shell log: empty home")
	}
	logDir := filepath.Join(home, ".runtime", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("shell log mkdir: %w", err)
	}
	path := filepath.Join(logDir, "shell.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("shell log open: %w", err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.SetFlags(log.Ldate | log.Ltime)
	return f, nil
}
