package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

var linuxTerminalSequence uint64

// OpenTerminal opens a persistent SSH PTY. Unlike Start, it does not run a
// one-shot command: the caller owns the terminal until Close or an explicit
// session expiry policy closes it.
func (p *LinuxShellProvider) OpenTerminal(ctx context.Context, req TerminalRequest) (Terminal, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.resolver == nil {
		return nil, fmt.Errorf("linux channel resolver is unavailable")
	}
	if req.Target.Kind != executionTargetLinuxChannel {
		return nil, fmt.Errorf("linux terminal provider does not support target %q", req.Target.Kind)
	}
	channelID := strings.TrimSpace(req.Target.ID)
	if channelID == "" {
		return nil, fmt.Errorf("linux channel target id is required")
	}
	if req.Rows < 0 || req.Cols < 0 {
		return nil, fmt.Errorf("terminal rows and columns must not be negative")
	}
	rows, cols := req.Rows, req.Cols
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}

	cfg, err := p.resolver.ResolveLinuxChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if err := validateLinuxChannelConfig(cfg); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("linux channel %q is disabled", cfg.ID)
	}
	if p.bindingResolver != nil {
		binding, err := p.bindingResolver.ResolveLinuxBinding(ctx, strings.TrimSpace(req.Context.AgentID), cfg.ID)
		if err != nil {
			return nil, err
		}
		if !binding.Enabled {
			return nil, fmt.Errorf("linux channel %q is not enabled for agent %q", cfg.ID, req.Context.AgentID)
		}
		if strings.TrimSpace(req.CWD) == "" && strings.TrimSpace(binding.RemoteCWD) != "" {
			cfg.DefaultCWD = strings.TrimSpace(binding.RemoteCWD)
		}
		if strings.TrimSpace(binding.Shell) != "" {
			cfg.RemoteShell = strings.TrimSpace(binding.Shell)
		}
	}
	if err := validateRemoteShell(req.Shell); err != nil {
		return nil, err
	}
	shell := strings.TrimSpace(req.Shell)
	if shell == "" {
		shell = strings.TrimSpace(cfg.RemoteShell)
	}
	if shell == "" {
		shell = "bash"
	}
	if err := validateRemoteShell(shell); err != nil {
		return nil, err
	}
	initInput, err := buildTerminalInit(cfg, req)
	if err != nil {
		return nil, err
	}

	cred, err := p.resolver.ResolveLinuxCredential(ctx, cfg.CredentialID)
	if err != nil {
		return nil, err
	}
	if !cred.Enabled {
		return nil, fmt.Errorf("linux credential %q is disabled", cred.ID)
	}
	auth, agentConn, err := p.authMethod(ctx, cred)
	if err != nil {
		return nil, err
	}
	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("linux channel connect failed: %w", err)
	}
	clientConn, chans, requests, err := ssh.NewClientConn(conn, net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         connectTimeout,
	})
	if err != nil {
		_ = conn.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("linux channel handshake failed: %w", err)
	}
	client := ssh.NewClient(clientConn, chans, requests)
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("linux channel session failed: %w", err)
	}
	if err := session.RequestPty("xterm", rows, cols, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		_ = session.Close()
		_ = client.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("linux terminal request PTY failed: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("linux terminal stdin failed: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("linux terminal output failed: %w", err)
	}

	contextValue := req.Context
	contextValue.Target = req.Target
	seq := atomic.AddUint64(&linuxTerminalSequence, 1)
	return &linuxTerminal{
		id:        fmt.Sprintf("linux-terminal-%d", seq),
		client:    client,
		session:   session,
		agentConn: agentConn,
		stdin:     stdin,
		stdout:    stdout,
		command:   shell + " -l",
		initInput: initInput,
		ctx:       contextValue,
		sink:      req.EventSink,
		rows:      rows,
		cols:      cols,
	}, nil
}

func validateRemoteShell(value string) error {
	shell := strings.TrimSpace(value)
	if shell == "" {
		return nil
	}
	if strings.ContainsAny(shell, " \t\r\n;|&") {
		return fmt.Errorf("invalid remote shell %q", shell)
	}
	return nil
}

func buildTerminalInit(cfg LinuxChannelConfig, req TerminalRequest) ([]byte, error) {
	cwd := strings.TrimSpace(req.CWD)
	if cwd == "" {
		cwd = strings.TrimSpace(cfg.DefaultCWD)
	}
	commands := make([]string, 0, 1+len(req.Env))
	if cwd != "" {
		commands = append(commands, "cd "+shellQuote(cwd))
	}
	keys := make([]string, 0, len(req.Env))
	for key := range req.Env {
		if !validEnvName(key) {
			return nil, fmt.Errorf("invalid remote environment variable %q", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		commands = append(commands, "export "+key+"="+shellQuote(req.Env[key]))
	}
	if len(commands) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(commands, " && ") + "\n"), nil
}

type linuxTerminal struct {
	mu        sync.Mutex
	inputMu   sync.Mutex
	id        string
	client    *ssh.Client
	session   *ssh.Session
	agentConn net.Conn
	stdin     io.WriteCloser
	stdout    io.Reader
	command   string
	initInput []byte
	ctx       ExecutionContext
	sink      ProcessEventSink
	seq       uint64
	rows      int
	cols      int
	started   bool
	closed    bool
	outputGot bool
	waitOnce  sync.Once
	waitErr   error
	exitMu    sync.RWMutex
	exit      *ExitStatus
	eventMu   sync.Mutex
}

func (t *linuxTerminal) ID() string {
	if t == nil {
		return ""
	}
	return t.id
}

func (t *linuxTerminal) Input(ctx context.Context, data []byte) error {
	if t == nil || t.stdin == nil {
		return fmt.Errorf("terminal is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	started, closed := t.started, t.closed
	t.mu.Unlock()
	if !started {
		return fmt.Errorf("terminal %s is not started", t.id)
	}
	if closed {
		return fmt.Errorf("terminal %s is closed", t.id)
	}
	if len(data) == 0 {
		return nil
	}
	t.inputMu.Lock()
	defer t.inputMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	if !t.started {
		t.mu.Unlock()
		return fmt.Errorf("terminal %s is not started", t.id)
	}
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("terminal %s is closed", t.id)
	}
	t.mu.Unlock()
	_, err := t.stdin.Write(data)
	return err
}

func (t *linuxTerminal) Output() (io.ReadCloser, error) {
	if t == nil || t.stdout == nil {
		return nil, fmt.Errorf("terminal is nil")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, fmt.Errorf("terminal %s is closed", t.id)
	}
	if t.outputGot {
		return nil, fmt.Errorf("terminal output has already been acquired")
	}
	t.outputGot = true
	return &linuxTerminalReader{Reader: t.stdout, terminal: t}, nil
}

func (t *linuxTerminal) Resize(ctx context.Context, rows, cols int) error {
	if t == nil || t.session == nil {
		return fmt.Errorf("terminal is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if rows <= 0 || cols <= 0 {
		return fmt.Errorf("terminal rows and columns must be positive")
	}
	t.mu.Lock()
	started, closed := t.started, t.closed
	t.mu.Unlock()
	if !started {
		return fmt.Errorf("terminal %s is not started", t.id)
	}
	if closed {
		return fmt.Errorf("terminal %s is closed", t.id)
	}
	if err := t.session.WindowChange(rows, cols); err != nil {
		return fmt.Errorf("linux terminal resize failed: %w", err)
	}
	t.mu.Lock()
	t.rows, t.cols = rows, cols
	t.mu.Unlock()
	return nil
}

func (t *linuxTerminal) Start() error {
	if t == nil || t.session == nil {
		return fmt.Errorf("terminal is nil")
	}
	t.inputMu.Lock()
	defer t.inputMu.Unlock()
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return fmt.Errorf("terminal %s already started", t.id)
	}
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("terminal %s is closed", t.id)
	}
	t.started = true
	t.mu.Unlock()
	if err := t.session.Start(t.command); err != nil {
		t.mu.Lock()
		t.started = false
		t.mu.Unlock()
		_ = t.Close()
		return err
	}
	t.emit(ProcessEventStarted, "", nil, nil)
	if len(t.initInput) > 0 {
		if _, err := t.stdin.Write(t.initInput); err != nil {
			_ = t.Close()
			return fmt.Errorf("linux terminal initialization failed: %w", err)
		}
	}
	return nil
}

func (t *linuxTerminal) Wait() error {
	if t == nil || t.session == nil {
		return fmt.Errorf("terminal is nil")
	}
	t.mu.Lock()
	started := t.started
	t.mu.Unlock()
	if !started {
		return fmt.Errorf("terminal %s is not started", t.id)
	}
	t.waitOnce.Do(func() {
		t.waitErr = t.session.Wait()
		exit := linuxExitStatus(t.waitErr)
		t.exitMu.Lock()
		t.exit = exit
		t.exitMu.Unlock()
		t.emit(ProcessEventExited, "", nil, exit)
	})
	return t.waitErr
}

func (t *linuxTerminal) ExitStatus() *ExitStatus {
	if t == nil {
		return nil
	}
	t.exitMu.RLock()
	defer t.exitMu.RUnlock()
	if t.exit == nil {
		return nil
	}
	copy := *t.exit
	return &copy
}

func (t *linuxTerminal) Terminate(ctx context.Context) error {
	if t == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.ExitStatus() != nil {
		return nil
	}
	t.emit(ProcessEventTerminateRequested, "", nil, nil)
	return t.Close()
}

func (t *linuxTerminal) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	session, client, agentConn := t.session, t.client, t.agentConn
	t.mu.Unlock()
	var first error
	if session != nil {
		if err := session.Close(); err != nil && !errors.Is(err, io.EOF) {
			first = err
		}
	}
	if client != nil {
		if err := client.Close(); err != nil && first == nil {
			first = err
		}
	}
	if agentConn != nil {
		_ = agentConn.Close()
	}
	return first
}

func (t *linuxTerminal) emit(kind ProcessEventType, stream string, data []byte, exit *ExitStatus) {
	if t == nil || t.sink == nil {
		return
	}
	t.eventMu.Lock()
	defer t.eventMu.Unlock()
	seq := atomic.AddUint64(&t.seq, 1)
	if len(data) > 0 {
		data = append([]byte(nil), data...)
	}
	t.sink(ProcessEvent{Type: kind, ProcessID: t.id, Seq: seq, Context: t.ctx, Stream: stream, Data: data, Exit: exit})
}

type linuxTerminalReader struct {
	io.Reader
	terminal *linuxTerminal
}

func (r *linuxTerminalReader) Read(data []byte) (int, error) {
	n, err := r.Reader.Read(data)
	if n > 0 && r.terminal != nil {
		r.terminal.emit(ProcessEventOutput, "pty", data[:n], nil)
	}
	return n, err
}

// Closing the output stream does not implicitly tear down the terminal. The
// caller must use Terminal.Close so a transient reader replacement cannot
// leave a remote SSH session half-cleaned-up.
func (r *linuxTerminalReader) Close() error { return nil }
