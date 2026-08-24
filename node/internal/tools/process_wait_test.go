package tools

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

type waitTestProcess struct {
	waitRelease chan struct{}
	termCalled  chan struct{}
	termErr     error
	closeOnce   sync.Once
}

func newWaitTestProcess() *waitTestProcess {
	return &waitTestProcess{
		waitRelease: make(chan struct{}),
		termCalled:  make(chan struct{}),
	}
}

func (p *waitTestProcess) ID() string { return "wait-test" }

func (p *waitTestProcess) StdoutPipe() (io.ReadCloser, error) {
	return io.NopCloser(nilReader{}), nil
}

func (p *waitTestProcess) StderrPipe() (io.ReadCloser, error) {
	return io.NopCloser(nilReader{}), nil
}

func (p *waitTestProcess) SetOutput(io.Writer, io.Writer) {}

func (p *waitTestProcess) Start() error { return nil }

func (p *waitTestProcess) Wait() error {
	<-p.waitRelease
	return nil
}

func (p *waitTestProcess) ExitStatus() *ExitStatus { return &ExitStatus{Code: 143} }

func (p *waitTestProcess) Terminate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-p.termCalled:
	default:
		close(p.termCalled)
	}
	p.closeOnce.Do(func() { close(p.waitRelease) })
	return p.termErr
}

func (p *waitTestProcess) Close() error { return nil }

type nilReader struct{}

func (nilReader) Read([]byte) (int, error) { return 0, io.EOF }

func TestWaitForProcessTerminatesWithLiveBoundedContext(t *testing.T) {
	process := newWaitTestProcess()
	started := time.Now()
	err, timedOut := waitForProcess(context.Background(), process, 10*time.Millisecond)
	if !timedOut {
		t.Fatal("expected timeout")
	}
	_ = err // A provider may report a clean Wait after Terminate; timedOut is authoritative.
	select {
	case <-process.termCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("process termination was not requested")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("wait exceeded command timeout by too much: %s", elapsed)
	}
}
