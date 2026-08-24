package tools

import (
	"context"
	"io"
	"time"
)

const processWaitGrace = 2 * time.Second

// waitForProcess keeps the command timeout authoritative even when a remote
// provider's Wait implementation does not unblock immediately after a
// terminate request.  Termination is deliberately isolated from the caller
// so a provider-specific cleanup/recovery routine cannot hold the tool call
// forever.
func waitForProcess(ctx context.Context, process Process, timeout time.Duration) (error, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = processWaitGrace
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	waitDone := make(chan error, 1)
	go func() { waitDone <- process.Wait() }()

	select {
	case err := <-waitDone:
		return err, false
	case <-commandCtx.Done():
		go func() {
			terminateCtx, terminateCancel := context.WithTimeout(context.Background(), processWaitGrace)
			defer terminateCancel()
			_ = process.Terminate(terminateCtx)
		}()
		// Give the provider a bounded opportunity to close its transport while
		// waiting for Wait in parallel. The caller must remain time-bounded even
		// if both operations are unhealthy.
		grace := time.NewTimer(processWaitGrace)
		defer grace.Stop()
		select {
		case err := <-waitDone:
			return err, true
		case <-grace.C:
			return commandCtx.Err(), true
		}
	}
}

// waitForOutputReaders drains normal process output but never lets a broken
// pipe/descendant keep the tool call open after the process has ended.
func waitForOutputReaders(done <-chan struct{}, closers ...io.Closer) {
	timer := time.NewTimer(processWaitGrace)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
		for _, closer := range closers {
			if closer != nil {
				_ = closer.Close()
			}
		}
		select {
		case <-done:
		case <-time.After(250 * time.Millisecond):
		}
	}
}
