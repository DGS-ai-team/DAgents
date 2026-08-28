package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

type linuxProcess struct {
	mu               sync.Mutex
	id               string
	provider         *LinuxShellProvider
	client           *ssh.Client
	session          *ssh.Session
	agentConn        net.Conn
	command          string
	ctx              ExecutionContext
	sink             ProcessEventSink
	seq              uint64
	started          bool
	closed           bool
	stdout           io.Reader
	stderr           io.Reader
	stdoutSet        bool
	stderrSet        bool
	stdoutOut        io.Writer
	stderrOut        io.Writer
	eventMu          sync.Mutex
	outputBytes      int64
	processGroupMode string
	remoteRecovery   RemoteProcessRecovery
	terminationMu    sync.RWMutex
	terminationState string
	exitMu           sync.RWMutex
	exit             *ExitStatus
	releaseSlot      func()
	releaseOnce      sync.Once
}

func (p *linuxProcess) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

func (p *linuxProcess) StdoutPipe() (io.ReadCloser, error) {
	if p == nil || p.session == nil {
		return nil, fmt.Errorf("process is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.stdoutSet || p.stdoutOut != nil {
		return nil, fmt.Errorf("stdout pipe must be configured before process start")
	}
	reader, err := p.session.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p.stdout = reader
	p.stdoutSet = true
	return &linuxProcessReader{Reader: reader, process: p, stream: "stdout"}, nil
}

func (p *linuxProcess) StderrPipe() (io.ReadCloser, error) {
	if p == nil || p.session == nil {
		return nil, fmt.Errorf("process is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.stderrSet || p.stderrOut != nil {
		return nil, fmt.Errorf("stderr pipe must be configured before process start")
	}
	reader, err := p.session.StderrPipe()
	if err != nil {
		return nil, err
	}
	p.stderr = reader
	p.stderrSet = true
	return &linuxProcessReader{Reader: reader, process: p, stream: "stderr"}, nil
}

func (p *linuxProcess) SetOutput(stdout, stderr io.Writer) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.stdoutSet || p.stderrSet {
		return
	}
	p.stdoutOut = stdout
	p.stderrOut = stderr
}

func (p *linuxProcess) Start() error {
	if p == nil || p.session == nil {
		return fmt.Errorf("process is nil")
	}
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return fmt.Errorf("process %s already started", p.id)
	}
	p.started = true
	if p.stdoutOut != nil {
		if p.sink != nil {
			p.session.Stdout = &linuxProcessWriter{Writer: p.stdoutOut, process: p, stream: "stdout"}
		} else {
			p.session.Stdout = p.stdoutOut
		}
	}
	if p.stderrOut != nil {
		if p.sink != nil {
			p.session.Stderr = &linuxProcessWriter{Writer: p.stderrOut, process: p, stream: "stderr"}
		} else {
			p.session.Stderr = p.stderrOut
		}
	}
	p.mu.Unlock()
	if err := p.session.Start(p.command); err != nil {
		_ = p.Close()
		return err
	}
	p.emit(ProcessEventStarted, "", nil, nil)
	return nil
}

func (p *linuxProcess) Wait() error {
	if p == nil || p.session == nil {
		return fmt.Errorf("process is nil")
	}
	err := p.session.Wait()
	exit := linuxExitStatus(err)
	p.exitMu.Lock()
	p.exit = exit
	p.exitMu.Unlock()
	p.emit(ProcessEventExited, "", nil, exit)
	return err
}

func (p *linuxProcess) ExitStatus() *ExitStatus {
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

func (p *linuxProcess) Terminate(_ context.Context) error {
	if p == nil {
		return nil
	}
	if p.ExitStatus() != nil {
		p.setTerminationState("confirmed")
		return nil
	}
	p.terminationMu.Lock()
	p.terminationState = "requested"
	p.terminationMu.Unlock()
	p.emit(ProcessEventTerminateRequested, "", nil, nil)

	p.mu.Lock()
	session := p.session
	closed := p.closed
	p.mu.Unlock()
	if closed {
		p.setTerminationState("unknown")
		return nil
	}
	var signalErr error
	if session != nil {
		// SIGINT gives the remote wrapper a chance to clean up the complete
		// process group before the SSH close fallback below.
		signalErr = session.Signal(ssh.SIGINT)
	}
	closeErr := p.Close()
	if p.provider != nil && p.remoteRecovery.JobToken != "" {
		// The original SSH session is intentionally closed before reconnecting:
		// the confirmation channel must not share a possibly half-closed session
		// with the process being terminated. This also covers a remote shell that
		// ignored SIGINT or detached its child after the first channel closed.
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		remoteStatus, recoveryErr := p.provider.RecoverRemoteProcess(recoveryCtx, p.ctx.AgentID, p.remoteRecovery)
		cancel()
		if recoveryErr == nil {
			switch remoteStatus {
			case "force_terminated":
				p.setTerminationState("force_terminated")
			case "terminated", "not_running":
				p.setTerminationState("confirmed")
			default:
				p.setTerminationState("unknown")
			}
			return closeErr
		}
	}
	p.setTerminationState("unknown")
	if signalErr != nil {
		return fmt.Errorf("remote termination signal was not acknowledged: %w", signalErr)
	}
	return closeErr
}

func (p *linuxProcess) setTerminationState(state string) {
	p.terminationMu.Lock()
	p.terminationState = state
	p.terminationMu.Unlock()
}

func (p *linuxProcess) TerminationState() string {
	if p == nil {
		return ""
	}
	p.terminationMu.RLock()
	defer p.terminationMu.RUnlock()
	return p.terminationState
}

func (p *linuxProcess) RemoteProcessRecovery() (RemoteProcessRecovery, bool) {
	if p == nil || p.remoteRecovery.JobToken == "" || p.remoteRecovery.PIDFile == "" {
		return RemoteProcessRecovery{}, false
	}
	return p.remoteRecovery, true
}

func (p *linuxProcess) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	session, client, agentConn := p.session, p.client, p.agentConn
	p.mu.Unlock()
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
	p.releaseOnce.Do(func() {
		if p.releaseSlot != nil {
			p.releaseSlot()
		}
	})
	return first
}

func (p *linuxProcess) emit(kind ProcessEventType, stream string, data []byte, exit *ExitStatus) {
	if p == nil || p.sink == nil {
		return
	}
	p.eventMu.Lock()
	defer p.eventMu.Unlock()
	seq := atomic.AddUint64(&p.seq, 1)
	if len(data) > 0 {
		data = append([]byte(nil), data...)
	}
	if kind == ProcessEventOutput {
		p.outputBytes += int64(len(data))
	}
	p.sink(ProcessEvent{
		Type:        kind,
		ProcessID:   p.id,
		Seq:         seq,
		Context:     p.ctx,
		Stream:      stream,
		Data:        data,
		OutputBytes: p.outputBytes,
		Exit:        exit,
	})
}

func linuxExitStatus(err error) *ExitStatus {
	status := &ExitStatus{Code: 0}
	if err == nil {
		return status
	}
	status.Code = 1
	status.Error = err.Error()
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		status.Code = exitErr.ExitStatus()
	}
	return status
}

type linuxProcessWriter struct {
	io.Writer
	process *linuxProcess
	stream  string
}

func (w *linuxProcessWriter) Write(data []byte) (int, error) {
	n, err := w.Writer.Write(data)
	if n > 0 {
		w.process.emit(ProcessEventOutput, w.stream, data[:n], nil)
	}
	return n, err
}

type linuxProcessReader struct {
	io.Reader
	process *linuxProcess
	stream  string
}

func (r *linuxProcessReader) Read(data []byte) (int, error) {
	n, err := r.Reader.Read(data)
	if n > 0 {
		r.process.emit(ProcessEventOutput, r.stream, data[:n], nil)
	}
	return n, err
}

func (r *linuxProcessReader) Close() error { return nil }
