package tools

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

type cancelledCommandProcess struct {
	waitDone chan struct{}
	once     sync.Once
	closed   chan struct{}
}

func (p *cancelledCommandProcess) ID() string { return "cancelled-command" }
func (p *cancelledCommandProcess) StdoutPipe() (io.ReadCloser, error) {
	return io.NopCloser(emptyProcessReader{}), nil
}
func (p *cancelledCommandProcess) StderrPipe() (io.ReadCloser, error) {
	return io.NopCloser(emptyProcessReader{}), nil
}
func (p *cancelledCommandProcess) SetOutput(io.Writer, io.Writer) {}
func (p *cancelledCommandProcess) Start() error                   { return nil }
func (p *cancelledCommandProcess) Wait() error {
	<-p.waitDone
	return context.Canceled
}
func (p *cancelledCommandProcess) ExitStatus() *ExitStatus { return nil }
func (p *cancelledCommandProcess) Terminate(context.Context) error {
	p.once.Do(func() {
		close(p.waitDone)
		close(p.closed)
	})
	return nil
}
func (p *cancelledCommandProcess) Close() error {
	p.once.Do(func() {
		close(p.waitDone)
		close(p.closed)
	})
	return nil
}

type emptyProcessReader struct{}

func (emptyProcessReader) Read([]byte) (int, error) { return 0, io.EOF }

type cancelledCommandProvider struct{ process *cancelledCommandProcess }

func (p *cancelledCommandProvider) Start(context.Context, ExecRequest) (Process, error) {
	return p.process, nil
}
func (p *cancelledCommandProvider) Test(context.Context, ExecutionTarget) (TargetStatus, error) {
	return TargetStatus{Available: true}, nil
}

func TestRunTerminalCommandCancellationReturnsTerminalState(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	process := &cancelledCommandProcess{waitDone: make(chan struct{}), closed: make(chan struct{})}
	if err := reg.WithShellProvider(&cancelledCommandProvider{process: process}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	result, err := reg.runTerminalCommand(ctx, TerminalCommandRequest{
		TerminalID:     "terminal-cancelled",
		Target:         ExecutionTarget{Kind: executionTargetLocal, ID: executionTargetLocal},
		Command:        "long-running",
		Timeout:        time.Minute,
		MaxOutputBytes: 1024,
	})
	if err != nil {
		t.Fatalf("runTerminalCommand error=%v", err)
	}
	if result.Status != "CANCELLED" || !result.Cancelled || result.TimedOut {
		t.Fatalf("unexpected cancellation result=%+v", result)
	}
	select {
	case <-process.closed:
	case <-time.After(time.Second):
		t.Fatal("cancelled process was not closed")
	}
}
