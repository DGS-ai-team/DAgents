package tools

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
)

type leaseTestTerminal struct {
	started    chan struct{}
	exited     chan struct{}
	closed     chan struct{}
	startBlock chan struct{}
	startErr   error
}

func (t *leaseTestTerminal) ID() string                             { return "fake" }
func (t *leaseTestTerminal) Input(context.Context, []byte) error    { return nil }
func (t *leaseTestTerminal) Output() (io.ReadCloser, error)         { return io.NopCloser(&emptyReader{}), nil }
func (t *leaseTestTerminal) Resize(context.Context, int, int) error { return nil }
func (t *leaseTestTerminal) Start() error {
	if t.startBlock != nil {
		<-t.startBlock
	}
	if t.startErr != nil {
		return t.startErr
	}
	close(t.started)
	return nil
}
func (t *leaseTestTerminal) Wait() error { <-t.exited; return nil }
func (t *leaseTestTerminal) ExitStatus() *ExitStatus {
	select {
	case <-t.exited:
		return &ExitStatus{}
	default:
		return nil
	}
}
func (t *leaseTestTerminal) Terminate(ctx context.Context) error { return ctx.Err() }
func (t *leaseTestTerminal) Close() error {
	select {
	case <-t.closed:
	default:
		close(t.closed)
	}
	return nil
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

func assertLeaseHeld(t *testing.T, c *workspacecoord.Coordinator, root string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if l, err := c.TryAcquire(ctx, root, "probe"); err == nil {
		l.Release()
		t.Fatal("lease unexpectedly released")
	}
}

func TestCoordinatedTerminalLifecycleBoundaries(t *testing.T) {
	root := t.TempDir()
	c := workspacecoord.New()
	fake := &leaseTestTerminal{started: make(chan struct{}), exited: make(chan struct{}), closed: make(chan struct{})}
	lease, err := c.TryAcquire(context.Background(), root, "terminal")
	if err != nil {
		t.Fatal(err)
	}
	w := &coordinatedTerminal{Terminal: fake, lease: lease}
	if err := w.Wait(); err == nil {
		t.Fatal("Wait before Start succeeded")
	}
	w.Close()
	probe, err := c.TryAcquire(context.Background(), root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	probe.Release()
	lease, _ = c.TryAcquire(context.Background(), root, "terminal")
	fake = &leaseTestTerminal{started: make(chan struct{}), exited: make(chan struct{}), closed: make(chan struct{})}
	w = &coordinatedTerminal{Terminal: fake, lease: lease}
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	assertLeaseHeld(t, c, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Terminate(ctx); err == nil {
		t.Fatal("cancelled Terminate succeeded")
	}
	assertLeaseHeld(t, c, root)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	assertLeaseHeld(t, c, root)
	close(fake.exited)
	if err := w.Wait(); err != nil {
		t.Fatal(err)
	}
	if l, err := c.TryAcquire(context.Background(), root, "probe"); err != nil {
		t.Fatal(err)
	} else {
		l.Release()
	}
}
