package tools

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
)

var localTerminalSequence uint64

// OpenTerminal opens a local native PTY. The platform-specific constructor
// supplies the actual PTY/process handles; lifecycle, events, and policy
// correlation remain identical to the SSH Terminal implementation.
func (p *LocalShellProvider) OpenTerminal(ctx context.Context, req TerminalRequest) (Terminal, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("local terminal provider is unavailable")
	}
	req, err := normalizeLocalTerminalRequest(req)
	if err != nil {
		return nil, err
	}
	return openLocalTerminal(ctx, req)
}

func normalizeLocalTerminalRequest(req TerminalRequest) (TerminalRequest, error) {
	if req.Target.Kind != "" && req.Target.Kind != executionTargetLocal {
		return TerminalRequest{}, fmt.Errorf("local terminal provider does not support target %q", req.Target.Kind)
	}
	req.Target.Kind = executionTargetLocal
	if strings.TrimSpace(req.Target.ID) == "" {
		req.Target.ID = executionTargetLocal
	}
	if req.Rows < 0 || req.Cols < 0 {
		return TerminalRequest{}, fmt.Errorf("terminal rows and columns must not be negative")
	}
	if req.Rows == 0 {
		req.Rows = 24
	}
	if req.Cols == 0 {
		req.Cols = 80
	}
	if shell := strings.TrimSpace(req.Shell); strings.ContainsAny(shell, "\x00\r\n\t;&|") {
		return TerminalRequest{}, fmt.Errorf("invalid local terminal shell %q", req.Shell)
	}
	for name, value := range req.Env {
		if !validEnvName(name) {
			return TerminalRequest{}, fmt.Errorf("invalid local environment variable %q", name)
		}
		if strings.ContainsRune(value, '\x00') {
			return TerminalRequest{}, fmt.Errorf("local environment variable %q contains NUL", name)
		}
	}
	req.Context.Target = req.Target
	return req, nil
}

func localEnvironment(extra map[string]string, policy EnvironmentPolicy) ([]string, error) {
	return buildShellEnvironment(extra, policy)
}

type localTerminal struct {
	mu          sync.Mutex
	inputMu     sync.Mutex
	id          string
	input       io.Writer
	output      io.Reader
	startFn     func() error
	waitFn      func() (*ExitStatus, error)
	closeFn     func() error
	resizeFn    func(rows, cols int) error
	ctx         ExecutionContext
	sink        ProcessEventSink
	seq         uint64
	started     bool
	closed      bool
	outputGot   bool
	waitOnce    sync.Once
	waitErr     error
	exitMu      sync.RWMutex
	exit        *ExitStatus
	eventMu     sync.Mutex
	outputBytes int64
}

func newLocalTerminal(req TerminalRequest, input io.Writer, output io.Reader, startFn func() error, waitFn func() (*ExitStatus, error), closeFn func() error, resizeFn func(rows, cols int) error, sink ProcessEventSink) *localTerminal {
	seq := atomic.AddUint64(&localTerminalSequence, 1)
	return &localTerminal{
		id:       fmt.Sprintf("local-terminal-%d", seq),
		input:    input,
		output:   output,
		startFn:  startFn,
		waitFn:   waitFn,
		closeFn:  closeFn,
		resizeFn: resizeFn,
		ctx:      req.Context,
		sink:     sink,
	}
}

func (t *localTerminal) ID() string {
	if t == nil {
		return ""
	}
	return t.id
}

func (t *localTerminal) Input(ctx context.Context, data []byte) error {
	if t == nil || t.input == nil {
		return fmt.Errorf("terminal is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	t.inputMu.Lock()
	defer t.inputMu.Unlock()
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
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := t.input.Write(data)
	return err
}

func (t *localTerminal) Output() (io.ReadCloser, error) {
	if t == nil || t.output == nil {
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
	return &localTerminalReader{Reader: t.output, terminal: t}, nil
}

func (t *localTerminal) Resize(ctx context.Context, rows, cols int) error {
	if t == nil || t.resizeFn == nil {
		return fmt.Errorf("terminal resize is unavailable")
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
	return t.resizeFn(rows, cols)
}

func (t *localTerminal) Start() error {
	if t == nil || t.startFn == nil {
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
	if err := t.startFn(); err != nil {
		t.mu.Lock()
		t.started = false
		t.mu.Unlock()
		_ = t.Close()
		return err
	}
	t.emit(ProcessEventStarted, "", nil, nil)
	return nil
}

func (t *localTerminal) Wait() error {
	if t == nil || t.waitFn == nil {
		return fmt.Errorf("terminal is nil")
	}
	t.mu.Lock()
	started := t.started
	t.mu.Unlock()
	if !started {
		return fmt.Errorf("terminal %s is not started", t.id)
	}
	t.waitOnce.Do(func() {
		exit, err := t.waitFn()
		t.exitMu.Lock()
		if exit == nil {
			exit = &ExitStatus{Code: -1}
		}
		t.exit = exit
		t.waitErr = err
		exitCopy := *exit
		t.exitMu.Unlock()
		t.emit(ProcessEventExited, "", nil, &exitCopy)
	})
	return t.waitErr
}

func (t *localTerminal) ExitStatus() *ExitStatus {
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

func (t *localTerminal) Terminate(ctx context.Context) error {
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

func (t *localTerminal) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	closeFn := t.closeFn
	t.mu.Unlock()
	if closeFn == nil {
		return nil
	}
	return closeFn()
}

func (t *localTerminal) emit(kind ProcessEventType, stream string, data []byte, exit *ExitStatus) {
	if t == nil || t.sink == nil {
		return
	}
	t.eventMu.Lock()
	defer t.eventMu.Unlock()
	seq := atomic.AddUint64(&t.seq, 1)
	if len(data) > 0 {
		data = append([]byte(nil), data...)
	}
	if kind == ProcessEventOutput {
		t.outputBytes += int64(len(data))
	}
	t.sink(ProcessEvent{
		Type:        kind,
		ProcessID:   t.id,
		Seq:         seq,
		Context:     t.ctx,
		Stream:      stream,
		Data:        data,
		OutputBytes: t.outputBytes,
		Exit:        exit,
	})
}

type localTerminalReader struct {
	io.Reader
	terminal *localTerminal
}

func (r *localTerminalReader) Read(data []byte) (int, error) {
	n, err := r.Reader.Read(data)
	if n > 0 && r.terminal != nil {
		r.terminal.emit(ProcessEventOutput, "pty", data[:n], nil)
	}
	return n, err
}

func (r *localTerminalReader) Close() error { return nil }

var _ TerminalProvider = (*LocalShellProvider)(nil)
