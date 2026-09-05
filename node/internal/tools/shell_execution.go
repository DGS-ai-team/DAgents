package tools

import (
	"sync"
	"time"
)

// shellExecution is the in-flight state for the synchronous bash_run path.
// It is deliberately local to one tool call: completion, timeout and UI
// cancellation all converge on this state and no result is persisted or
// re-injected as a second tool message.
type shellExecution struct {
	mu                  sync.Mutex
	status              string
	result              string
	finishedAt          int64
	done                chan struct{}
	process             Process
	bashCwd             string
	bashTimeout         int
	bashStdout          string
	bashStderr          string
	bashOutputTruncated bool
	bashExitCode        *int
	bashShellType       string
	bashOutputEncoding  string
	compressStats       *OutputCompressStats
}

const (
	shellStatusRunning   = "running"
	shellStatusSucceeded = "succeeded"
	shellStatusFailed    = "failed"
	shellStatusCancelled = "cancelled"
)

func (execution *shellExecution) transitionStatusLocked(next, result string) bool {
	if execution == nil || execution.status != shellStatusRunning {
		return false
	}
	if next != shellStatusSucceeded && next != shellStatusFailed && next != shellStatusCancelled {
		return false
	}
	execution.status = next
	if result != "" {
		execution.result = result
	}
	execution.finishedAt = time.Now().UnixMilli()
	return true
}
