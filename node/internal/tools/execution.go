package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ExecutionTarget identifies the execution world selected by the policy and
// orchestration layers. The local provider is the first implementation; the
// target fields leave room for SSH, container, and exec-server providers
// without changing the model-facing bash_run schema.
type ExecutionTarget struct {
	Kind string
	ID   string
}

const executionTargetLocal = "local"

// ExecutionContext carries the correlation fields needed by execution
// events. It is deliberately separate from the model-facing tool arguments.
type ExecutionContext struct {
	AgentID        string
	SessionID      string
	TurnID         string
	ToolCallID     string
	Target         ExecutionTarget
	PolicyDecision string
	ApprovalID     string
	RiskLevel      string
}

// ExecRequest is the internal request passed from a tool to an execution
// provider. Command keeps shell syntax (pipelines, redirects, and scripts),
// while Argv is reserved for future direct process execution.
type ExecRequest struct {
	Target         ExecutionTarget
	Context        ExecutionContext
	ShellType      string
	Command        string
	Argv           []string
	CWD            string
	Env            map[string]string
	TTY            bool
	PipeStdin      bool
	Timeout        time.Duration
	MaxOutputBytes int
	EventSink      ProcessEventSink
}

// TargetStatus describes provider reachability. It is intentionally small in
// P0; remote providers can add richer diagnostics without changing the
// execution request or process lifecycle.
type TargetStatus struct {
	Available bool
	Message   string
}

type ProcessEventType string

const (
	ProcessEventStarted            ProcessEventType = "process_started"
	ProcessEventOutput             ProcessEventType = "process_output"
	ProcessEventTerminateRequested ProcessEventType = "process_terminate_requested"
	ProcessEventExited             ProcessEventType = "process_exited"
)

// ExitStatus is independent of os.ProcessState so remote providers can expose
// the same lifecycle event without pretending to have a local process.
type ExitStatus struct {
	Code  int
	Error string
}

// ProcessEvent is a runtime execution event. Seq is monotonic per process;
// output bytes are kept separate by Stream and are not mixed with stderr.
type ProcessEvent struct {
	Type      ProcessEventType
	ProcessID string
	Seq       uint64
	Context   ExecutionContext
	Stream    string
	Data      []byte
	Exit      *ExitStatus
}

type ProcessEventSink func(ProcessEvent)

// Process is the provider-neutral lifecycle handle used by synchronous and
// background shell execution. Pipes are acquired before Start so callers can
// keep the current stdout/stderr collection and auto-degrade behavior.
type Process interface {
	ID() string
	StdoutPipe() (io.ReadCloser, error)
	StderrPipe() (io.ReadCloser, error)
	SetOutput(stdout, stderr io.Writer)
	Start() error
	Wait() error
	ExitStatus() *ExitStatus
	Terminate(ctx context.Context) error
	Close() error
}

// ShellProvider creates process handles for a selected execution target.
// Policy, HITL, history, and tool-result formatting remain outside providers.
type ShellProvider interface {
	Start(ctx context.Context, req ExecRequest) (Process, error)
	Test(ctx context.Context, target ExecutionTarget) (TargetStatus, error)
}

// LocalShellProvider executes a shell command on the current Node host. It
// owns only process construction and lifecycle; bash_run still owns timeout,
// output decoding, compression, and background-job semantics.
type LocalShellProvider struct{}

var localProcessSequence uint64

func NewLocalShellProvider() *LocalShellProvider {
	return &LocalShellProvider{}
}

func (p *LocalShellProvider) Start(_ context.Context, req ExecRequest) (Process, error) {
	if target := req.Target.Kind; target != "" && target != executionTargetLocal {
		return nil, fmt.Errorf("local shell provider does not support target %q", target)
	}
	if req.TTY {
		return nil, fmt.Errorf("local shell provider does not support TTY yet")
	}
	if req.PipeStdin {
		return nil, fmt.Errorf("local shell provider does not support piped stdin yet")
	}
	if strings.TrimSpace(req.Command) == "" && len(req.Argv) == 0 {
		return nil, fmt.Errorf("execution command is required")
	}
	if strings.TrimSpace(req.Command) != "" && len(req.Argv) > 0 {
		return nil, fmt.Errorf("execution request cannot contain both command and argv")
	}

	var cmd *exec.Cmd
	var err error
	if len(req.Argv) > 0 {
		cmd = exec.Command(req.Argv[0], req.Argv[1:]...)
	} else {
		var shellType *string
		if strings.TrimSpace(req.ShellType) != "" {
			value := req.ShellType
			shellType = &value
		}
		st, resolveErr := resolveShellType(shellType)
		if resolveErr != nil {
			return nil, resolveErr
		}
		cmd, err = buildShellCommand(st, req.Command)
		if err != nil {
			return nil, err
		}
	}
	applyShellCmdDir(cmd, req.CWD)
	if len(req.Env) > 0 {
		env := os.Environ()
		for key, value := range req.Env {
			env = append(env, key+"="+value)
		}
		cmd.Env = env
	}

	seq := atomic.AddUint64(&localProcessSequence, 1)
	return &localProcess{
		id:   fmt.Sprintf("local-process-%d", seq),
		cmd:  cmd,
		ctx:  req.Context,
		sink: req.EventSink,
	}, nil
}

func (p *LocalShellProvider) Test(ctx context.Context, target ExecutionTarget) (TargetStatus, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return TargetStatus{}, ctx.Err()
		default:
		}
	}
	if target.Kind != "" && target.Kind != executionTargetLocal {
		return TargetStatus{Message: fmt.Sprintf("unsupported target %q", target.Kind)}, nil
	}
	return TargetStatus{Available: true, Message: "local execution is available"}, nil
}

type localProcess struct {
	mu      sync.Mutex
	id      string
	cmd     *exec.Cmd
	tree    processTreeHandle
	ctx     ExecutionContext
	sink    ProcessEventSink
	seq     uint64
	eventMu sync.Mutex
	start   bool
	close   bool
	exitMu  sync.RWMutex
	exit    *ExitStatus
}

func (p *localProcess) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

func (p *localProcess) StdoutPipe() (io.ReadCloser, error) {
	if p == nil || p.cmd == nil {
		return nil, fmt.Errorf("process is nil")
	}
	pipe, err := p.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	return &processEventReader{ReadCloser: pipe, process: p, stream: "stdout"}, nil
}

func (p *localProcess) StderrPipe() (io.ReadCloser, error) {
	if p == nil || p.cmd == nil {
		return nil, fmt.Errorf("process is nil")
	}
	pipe, err := p.cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	return &processEventReader{ReadCloser: pipe, process: p, stream: "stderr"}, nil
}

func (p *localProcess) SetOutput(stdout, stderr io.Writer) {
	if p == nil || p.cmd == nil {
		return
	}
	if p.sink == nil {
		p.cmd.Stdout = stdout
		p.cmd.Stderr = stderr
		return
	}
	p.cmd.Stdout = &processEventWriter{Writer: stdout, process: p, stream: "stdout"}
	p.cmd.Stderr = &processEventWriter{Writer: stderr, process: p, stream: "stderr"}
}

func (p *localProcess) Start() error {
	if p == nil || p.cmd == nil {
		return fmt.Errorf("process is nil")
	}
	p.mu.Lock()
	if p.start {
		p.mu.Unlock()
		return fmt.Errorf("process %s already started", p.id)
	}
	p.start = true
	p.mu.Unlock()

	p.eventMu.Lock()
	defer p.eventMu.Unlock()
	if err := p.cmd.Start(); err != nil {
		return err
	}
	tree, err := attachProcessTree(p.cmd)
	if err == nil {
		p.mu.Lock()
		p.tree = tree
		p.mu.Unlock()
	}
	// A missing process-tree capability must not prevent the shell itself from
	// running. Termination falls back to the platform's shell kill behavior.
	p.emitLocked(ProcessEventStarted, "", nil, nil)
	return nil
}

func (p *localProcess) Wait() error {
	if p == nil || p.cmd == nil {
		return fmt.Errorf("process is nil")
	}
	err := p.cmd.Wait()
	exit := processExitStatus(p.cmd.ProcessState, err)
	p.exitMu.Lock()
	p.exit = exit
	p.exitMu.Unlock()
	p.emit(ProcessEventExited, "", nil, exit)
	return err
}

func (p *localProcess) ExitStatus() *ExitStatus {
	if p == nil {
		return nil
	}
	p.exitMu.RLock()
	defer p.exitMu.RUnlock()
	if p.exit == nil {
		return nil
	}
	copy := *p.exit
	return &copy
}

func (p *localProcess) Terminate(_ context.Context) error {
	if p == nil || p.cmd == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd.Process == nil || p.cmd.ProcessState != nil {
		return nil
	}
	// Serialize termination with Close so a Windows Job Object cannot be
	// closed between reading the handle and terminating the process tree.
	terminateProcessTree(p.cmd, p.tree)
	p.emit(ProcessEventTerminateRequested, "", nil, nil)
	return nil
}

func (p *localProcess) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.close {
		p.mu.Unlock()
		return nil
	}
	p.close = true
	tree := p.tree
	p.tree = nil
	p.mu.Unlock()
	closeProcessTree(tree)
	return nil
}

func (p *localProcess) emit(kind ProcessEventType, stream string, data []byte, exit *ExitStatus) {
	if p == nil || p.sink == nil {
		return
	}
	p.eventMu.Lock()
	defer p.eventMu.Unlock()
	p.emitLocked(kind, stream, data, exit)
}

func (p *localProcess) emitLocked(kind ProcessEventType, stream string, data []byte, exit *ExitStatus) {
	if p == nil || p.sink == nil {
		return
	}
	seq := atomic.AddUint64(&p.seq, 1)
	if len(data) > 0 {
		data = append([]byte(nil), data...)
	}
	p.sink(ProcessEvent{
		Type:      kind,
		ProcessID: p.id,
		Seq:       seq,
		Context:   p.ctx,
		Stream:    stream,
		Data:      data,
		Exit:      exit,
	})
}

func processExitStatus(state *os.ProcessState, err error) *ExitStatus {
	status := &ExitStatus{Code: -1}
	if state != nil {
		status.Code = state.ExitCode()
	}
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

type processEventWriter struct {
	io.Writer
	process *localProcess
	stream  string
}

func (w *processEventWriter) Write(data []byte) (int, error) {
	n, err := w.Writer.Write(data)
	if n > 0 {
		w.process.emit(ProcessEventOutput, w.stream, data[:n], nil)
	}
	return n, err
}

type processEventReader struct {
	io.ReadCloser
	process *localProcess
	stream  string
}

func (r *processEventReader) Read(data []byte) (int, error) {
	n, err := r.ReadCloser.Read(data)
	if n > 0 {
		r.process.emit(ProcessEventOutput, r.stream, data[:n], nil)
	}
	return n, err
}
