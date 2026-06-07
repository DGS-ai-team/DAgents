package triggers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const defaultCmdGateTimeout = 30 * time.Second

// CmdGate 在 schedule 触发前执行可选 cmd 门控。
type CmdGate interface {
	Run(cmd string) (ok bool, detail string, err error)
}

// ShellCmdGate 使用主机 shell 执行 cmd；exit 0 视为通过。
type ShellCmdGate struct {
	Timeout time.Duration
}

// NewShellCmdGate 构造默认 shell 门控。
func NewShellCmdGate() *ShellCmdGate {
	return &ShellCmdGate{Timeout: defaultCmdGateTimeout}
}

// Run 执行 cmd 并返回 exit 0 是否通过。

// 异常：启动失败/超时返回 err；非零 exit 返回 ok=false 且无 err。
func (g *ShellCmdGate) Run(cmd string) (bool, string, error) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return true, "", nil
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = defaultCmdGateTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", cmd)
	} else {
		c = exec.CommandContext(ctx, "bash", "-lc", cmd)
	}
	c.Env = os.Environ()
	out, err := c.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		return false, text, fmt.Errorf("cmd gate timeout after %s", timeout)
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return false, fmt.Sprintf("exit_code=%d output=%s", exitErr.ExitCode(), text), nil
		}
		return false, text, err
	}
	if text == "" {
		text = "exit 0"
	}
	return true, text, nil
}
