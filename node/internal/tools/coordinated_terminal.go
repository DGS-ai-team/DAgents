package tools

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
)

type coordinatedTerminal struct {
	Terminal
	lease          *workspacecoord.Lease
	once           sync.Once
	mu             sync.Mutex
	state          string
	done           chan struct{}
	waitErr        error
	closeRequested bool
}

func (t *coordinatedTerminal) release() { t.once.Do(func() { t.lease.Release() }) }
func (t *coordinatedTerminal) Start() error {
	t.mu.Lock()
	if t.state == "" {
		t.state = "new"
	}
	if t.state != "new" {
		state := t.state
		t.mu.Unlock()
		return fmt.Errorf("terminal cannot start from state %s", state)
	}
	t.state = "starting"
	t.mu.Unlock()
	if err := t.Terminal.Start(); err != nil {
		t.mu.Lock()
		t.state = "failed"
		t.mu.Unlock()
		t.release()
		return err
	}
	t.mu.Lock()
	t.state = "running"
	t.done = make(chan struct{})
	done := t.done
	closeRequested := t.closeRequested
	t.mu.Unlock()
	go func() {
		err := t.Terminal.Wait()
		t.mu.Lock()
		t.waitErr = err
		t.state = "exited"
		close(done)
		t.mu.Unlock()
		t.release()
	}()
	if closeRequested {
		_ = t.Terminal.Close()
	}
	return nil
}
func (t *coordinatedTerminal) Wait() error {
	t.mu.Lock()
	state, done := t.state, t.done
	t.mu.Unlock()
	if state == "" || state == "new" || state == "starting" {
		return fmt.Errorf("terminal is not started")
	}
	if state == "failed" || state == "closed" {
		return fmt.Errorf("terminal is %s", state)
	}
	<-done
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.waitErr
}
func (t *coordinatedTerminal) Terminate(ctx context.Context) error {
	return t.Terminal.Terminate(ctx)
}
func (t *coordinatedTerminal) Close() error {
	t.mu.Lock()
	state := t.state
	if state == "starting" {
		t.closeRequested = true
		t.mu.Unlock()
		return t.Terminal.Close()
	}
	if state == "" || state == "new" {
		t.state = "closed"
		t.mu.Unlock()
		err := t.Terminal.Close()
		t.release()
		return err
	}
	t.mu.Unlock()
	return t.Terminal.Close()
}

var _ Terminal = (*coordinatedTerminal)(nil)
var _ io.Closer = (*coordinatedTerminal)(nil)
