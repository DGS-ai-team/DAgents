//go:build !windows

package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

func openLocalTerminal(ctx context.Context, req TerminalRequest) (Terminal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	shell, args, err := resolveUnixTerminalShell(req.Shell)
	if err != nil {
		return nil, err
	}
	master, tty, err := pty.Open()
	if err != nil {
		return nil, fmt.Errorf("open local PTY: %w", err)
	}
	if err := pty.Setsize(master, &pty.Winsize{Rows: uint16(req.Rows), Cols: uint16(req.Cols)}); err != nil {
		_ = master.Close()
		_ = tty.Close()
		return nil, fmt.Errorf("size local PTY: %w", err)
	}
	cmd := exec.Command(shell, args...)
	cmd.Dir = strings.TrimSpace(req.CWD)
	cmd.Env = localEnvironment(req.Env)
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	state := &unixLocalTerminalState{cmd: cmd, master: master, tty: tty}
	return newLocalTerminal(
		req,
		master,
		master,
		state.start,
		state.wait,
		state.close,
		state.resize,
		req.EventSink,
	), nil
}

func resolveUnixTerminalShell(raw string) (string, []string, error) {
	shell := strings.TrimSpace(raw)
	if shell == "" {
		shell = "bash"
	}
	switch strings.ToLower(shell) {
	case "bash":
		return "bash", []string{"-l"}, nil
	case "sh":
		return "sh", []string{"-l"}, nil
	case "zsh":
		return "zsh", []string{"-l"}, nil
	}
	if strings.ContainsAny(shell, " \t\r\n;|&") {
		return "", nil, fmt.Errorf("invalid local terminal shell %q", raw)
	}
	path, err := exec.LookPath(shell)
	if err != nil {
		return "", nil, fmt.Errorf("local terminal shell %q is unavailable: %w", shell, err)
	}
	return path, []string{"-l"}, nil
}

type unixLocalTerminalState struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	master *os.File
	tty    *os.File
	tree   processTreeHandle
}

func (s *unixLocalTerminalState) start() error {
	if s == nil || s.cmd == nil {
		return fmt.Errorf("local terminal process is unavailable")
	}
	if err := s.cmd.Start(); err != nil {
		return err
	}
	if s.tty != nil {
		_ = s.tty.Close()
		s.tty = nil
	}
	tree, _ := attachProcessTree(s.cmd)
	s.mu.Lock()
	s.tree = tree
	s.mu.Unlock()
	return nil
}

func (s *unixLocalTerminalState) wait() (*ExitStatus, error) {
	if s == nil || s.cmd == nil {
		return &ExitStatus{Code: -1, Error: "local terminal process is unavailable"}, fmt.Errorf("local terminal process is unavailable")
	}
	err := s.cmd.Wait()
	return processExitStatus(s.cmd.ProcessState, err), err
}

func (s *unixLocalTerminalState) resize(rows, cols int) error {
	if s == nil || s.master == nil {
		return fmt.Errorf("local PTY is unavailable")
	}
	return pty.Setsize(s.master, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (s *unixLocalTerminalState) close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	tree := s.tree
	s.tree = nil
	cmd, master, tty := s.cmd, s.master, s.tty
	s.master, s.tty = nil, nil
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil && cmd.ProcessState == nil {
		terminateProcessTree(cmd, tree)
	}
	closeProcessTree(tree)
	var first error
	if master != nil {
		if err := master.Close(); err != nil {
			first = err
		}
	}
	if tty != nil {
		if err := tty.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
