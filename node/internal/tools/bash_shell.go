package tools

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

type shellType string

const (
	shellBash        shellType = "bash"
	shellCmd         shellType = "cmd"
	shellPowerShell  shellType = "powershell"
	defaultOutputEnc           = "utf-8"

	// powerShellPipeEncodingPrefix 在 pipe 模式下统一 Console 与 $OutputEncoding 为 UTF-8。
	// PS 5.1 默认 $OutputEncoding 常为 US-ASCII，Console 为 GBK；外部 exe（如 agent-browser）与
	// cmdlet 输出编码不一致时会在 Go 解码前损坏。UTF-8 可同时覆盖 PS 内置中文与 UTF-8 工具输出。
	powerShellPipeEncodingPrefix = "$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false); "
)

// resolveShellType 解析最终 shell：显式优先，否则 Windows→powershell，其余→bash。
func resolveShellType(raw *string) (shellType, error) {
	if raw != nil {
		st := shellType(strings.ToLower(strings.TrimSpace(*raw)))
		switch st {
		case shellBash, shellCmd, shellPowerShell:
			return st, nil
		default:
			return "", fmt.Errorf("不支持的 shell_type：%q", *raw)
		}
	}
	if runtime.GOOS == "windows" {
		return shellPowerShell, nil
	}
	return shellBash, nil
}

// resolveRunCWD 解析执行目录；空则 workspace root，相对路径在 workspace 下展开。

// 关键分支：路径必须落在 workspace root 内且为已存在目录。
func (r *Registry) resolveRunCWD(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return r.workspaceRoot, nil
	}
	if strings.HasPrefix(raw, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~ failed: %w", err)
		}
		if raw == "~" {
			raw = home
		} else if strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, "~\\") {
			raw = filepath.Join(home, raw[2:])
		}
	}
	var target string
	if filepath.IsAbs(raw) {
		target = filepath.Clean(raw)
	} else {
		target = filepath.Join(r.workspaceRoot, filepath.Clean(raw))
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	root := r.workspaceRoot
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		return "", fmt.Errorf("cwd escapes workspace root: %s", raw)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("cwd 不存在：%q", abs)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd 不是目录：%q", abs)
	}
	return abs, nil
}

// wrapShellCommandForPipe 在子进程 stdout 被 pipe 捕获前对齐 shell 输出编码（见 powerShellPipeEncodingPrefix）。
func wrapShellCommandForPipe(st shellType, command string) string {
	if runtime.GOOS != "windows" {
		return command
	}
	if st == shellPowerShell {
		return powerShellPipeEncodingPrefix + command
	}
	return command
}

// encodePowerShellCommand converts a complete PowerShell script to the format
// accepted by -EncodedCommand. This avoids concatenating a runner prefix and
// user input into a command-line string, which otherwise creates a second
// quoting layer for quotes, backticks, $, JSON and non-ASCII paths.
func encodePowerShellCommand(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, len(units)*2)
	for i, unit := range units {
		buf[i*2] = byte(unit)
		buf[i*2+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// buildShellCommand 按 shell 类型构造 *exec.Cmd 参数（不含 Start）。

// powershell 按 pwsh → powershell 顺序探测；均不存在时返回 error。
func buildShellCommand(st shellType, command string) (*exec.Cmd, error) {
	switch st {
	case shellBash:
		return exec.Command("bash", "-lc", wrapShellCommandForPipe(st, command)), nil
	case shellCmd:
		return exec.Command("cmd", "/C", wrapShellCommandForPipe(st, command)), nil
	case shellPowerShell:
		command = wrapShellCommandForPipe(st, command)
		if path, err := exec.LookPath("pwsh"); err == nil {
			return exec.Command(path, "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShellCommand(command)), nil
		}
		if path, err := exec.LookPath("powershell"); err == nil {
			return exec.Command(path, "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShellCommand(command)), nil
		}
		return nil, fmt.Errorf("未找到 powershell/pwsh 可执行文件")
	default:
		return nil, fmt.Errorf("unsupported shell: %s", st)
	}
}

// applyShellCmdDir 设置工作目录；POSIX 下额外配置独立进程组便于 cancel。
func applyShellCmdDir(cmd *exec.Cmd, cwd string) {
	cmd.Dir = cwd
	applyShellProcAttr(cmd)
}
