//go:build windows

package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/UserExistsError/conpty"
)

func openLocalTerminal(ctx context.Context, req TerminalRequest) (Terminal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	application, args, err := resolveWindowsTerminalShell(req.Shell)
	if err != nil {
		return nil, err
	}
	env, err := localEnvironment(req.Env, req.Environment)
	if err != nil {
		return nil, err
	}
	commandLine := make([]string, 0, 1+len(args))
	commandLine = append(commandLine, syscall.EscapeArg(application))
	for _, arg := range args {
		commandLine = append(commandLine, syscall.EscapeArg(arg))
	}
	state := &windowsLocalTerminalState{
		ready:       make(chan struct{}),
		commandLine: strings.Join(commandLine, " "),
		cwd:         strings.TrimSpace(req.CWD),
		env:         env,
		rows:        req.Rows,
		cols:        req.Cols,
	}
	return newLocalTerminal(req, state, state, state.start, state.wait, state.close, state.resize, req.EventSink), nil
}

func resolveWindowsTerminalShell(raw string) (string, []string, error) {
	shell := strings.ToLower(strings.TrimSpace(raw))
	switch shell {
	case "", "powershell", "pwsh":
		for _, candidate := range []string{"pwsh.exe", "powershell.exe", "pwsh", "powershell"} {
			if path, err := exec.LookPath(candidate); err == nil {
				return path, []string{"-NoLogo", "-NoProfile"}, nil
			}
		}
		return "", nil, fmt.Errorf("未找到 powershell/pwsh 可执行文件")
	case "cmd":
		path, err := exec.LookPath("cmd.exe")
		if err != nil {
			return "", nil, fmt.Errorf("未找到 cmd.exe: %w", err)
		}
		return path, []string{"/Q"}, nil
	case "bash":
		for _, candidate := range []string{"bash.exe", "bash"} {
			if path, err := exec.LookPath(candidate); err == nil {
				return path, []string{"--login"}, nil
			}
		}
		return "", nil, fmt.Errorf("未找到 bash/bash.exe")
	case "wsl":
		for _, candidate := range []string{"wsl.exe", "wsl"} {
			if path, err := exec.LookPath(candidate); err == nil {
				return path, nil, nil
			}
		}
		return "", nil, fmt.Errorf("未找到 wsl.exe")
	}
	if strings.ContainsAny(shell, " \t\r\n;|&") {
		return "", nil, fmt.Errorf("invalid local terminal shell %q", raw)
	}
	path, err := exec.LookPath(shell)
	if err != nil {
		return "", nil, fmt.Errorf("local terminal shell %q is unavailable: %w", shell, err)
	}
	return path, nil, nil
}

type windowsLocalTerminalState struct {
	mu          sync.Mutex
	ready       chan struct{}
	readyOnce   sync.Once
	closed      bool
	pty         *conpty.ConPty
	commandLine string
	cwd         string
	env         []string
	rows        int
	cols        int
}

func (s *windowsLocalTerminalState) start() error {
	if s == nil {
		return fmt.Errorf("windows ConPTY is unavailable")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("local terminal is closed")
	}
	s.mu.Unlock()
	options := []conpty.ConPtyOption{
		conpty.ConPtyDimensions(s.cols, s.rows),
	}
	if s.cwd != "" {
		options = append(options, conpty.ConPtyWorkDir(s.cwd))
	}
	if s.env != nil {
		options = append(options, conpty.ConPtyEnv(s.env))
	}
	pty, err := conpty.Start(s.commandLine, options...)
	if err != nil {
		return fmt.Errorf("start local ConPTY: %w", err)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = pty.Close()
		return fmt.Errorf("local terminal is closed")
	}
	s.pty = pty
	s.mu.Unlock()
	s.readyOnce.Do(func() { close(s.ready) })
	return nil
}

func (s *windowsLocalTerminalState) waitPTY() (*conpty.ConPty, error) {
	if s == nil {
		return nil, fmt.Errorf("windows ConPTY is unavailable")
	}
	<-s.ready
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pty == nil {
		return nil, fmt.Errorf("local ConPTY process is unavailable")
	}
	return s.pty, nil
}

func (s *windowsLocalTerminalState) Read(data []byte) (int, error) {
	pty, err := s.waitPTY()
	if err != nil {
		return 0, err
	}
	return pty.Read(data)
}

func (s *windowsLocalTerminalState) Write(data []byte) (int, error) {
	pty, err := s.waitPTY()
	if err != nil {
		return 0, err
	}
	return pty.Write(data)
}

func (s *windowsLocalTerminalState) wait() (*ExitStatus, error) {
	pty, err := s.waitPTY()
	if err != nil {
		return &ExitStatus{Code: -1, Error: err.Error()}, err
	}
	code, err := pty.Wait(context.Background())
	if err != nil {
		return &ExitStatus{Code: -1, Error: err.Error()}, err
	}
	return &ExitStatus{Code: int(code)}, nil
}

func (s *windowsLocalTerminalState) resize(rows, cols int) error {
	pty, err := s.waitPTY()
	if err != nil {
		return err
	}
	return pty.Resize(cols, rows)
}

func (s *windowsLocalTerminalState) close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	pty := s.pty
	s.pty = nil
	s.mu.Unlock()
	s.readyOnce.Do(func() { close(s.ready) })
	if pty != nil {
		return pty.Close()
	}
	return nil
}
