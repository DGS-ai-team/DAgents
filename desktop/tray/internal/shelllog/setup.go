// Package shelllog 将 Shell 标准 log 输出落盘到按日区分的 shell-*.log（完整日志）。
package shelllog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/logfiles"
)

// Setup 追加写入当日 shell-YYYY-MM-DD.log；错误级输出由进程 stderr 重定向至 *.err.log。
// 返回 closer（可 nil）。
func Setup(home string) (io.Closer, error) {
	home = filepath.Clean(home)
	if home == "" {
		return nil, fmt.Errorf("shell log: empty home")
	}
	logDir := filepath.Join(home, ".runtime", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("shell log mkdir: %w", err)
	}
	path := logfiles.JoinDated(logDir, "shell", false, time.Now())
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("shell log open: %w", err)
	}
	// 只写入完整日志文件，避免再 tee 到 stderr（后台启动时 stderr 会进 *.err.log，导致全量污染错误日志）。
	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime)
	return f, nil
}
