// Package childagent 实现临时子 Agent 生命周期、工具与 SSE（见 docs/architecture/child-agent-tools.md）。
package childagent

import (
	"sync"
	"time"
)

// Status 为子 Agent 生命周期状态。
type Status string

const (
	StatusCreating  Status = "creating"
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	StatusExpired   Status = "expired"
)

// CreateInput 为 create_temporary_agent 工具入参。
type CreateInput struct {
	Task         string
	Purpose      string
	AllowedTools []string
	TTLSeconds   int
	MaxTurns int
	Wait     bool
}

// Result 为交付给父 Agent 的终态结果。
type Result struct {
	ChildSessionID string   `json:"child_session_id"`
	Status         Status   `json:"status"`
	Summary        string   `json:"summary"`
	TurnCount      int      `json:"turn_count"`
	Error          string   `json:"error,omitempty"`
	Artifacts      []string `json:"artifacts"`
}

// Record 跟踪单个子 Agent 元数据与等待方。
type Record struct {
	ChildSessionID  string
	ParentSessionID string
	Purpose         string
	AllowedTools    []string
	Status          Status
	CreatedAt       time.Time
	ExpiresAt       time.Time
	MaxTurns        int
	TurnCount int
	WaitSync  bool

	mu     sync.Mutex
	result *Result
	done   chan struct{}
}

func newRecord(parentID string, input CreateInput, childID string, expiresAt time.Time) *Record {
	return &Record{
		ChildSessionID:  childID,
		ParentSessionID: parentID,
		Purpose:         input.Purpose,
		AllowedTools:    append([]string(nil), input.AllowedTools...),
		Status:          StatusCreating,
		CreatedAt:       time.Now(),
		ExpiresAt:       expiresAt,
		MaxTurns: input.MaxTurns,
		WaitSync: input.Wait,
		done:            make(chan struct{}),
	}
}

func (r *Record) terminal() bool {
	switch r.Status {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusExpired:
		return true
	default:
		return false
	}
}

func (r *Record) snapshot() Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.result != nil {
		out := *r.result
		return out
	}
	return Result{
		ChildSessionID: r.ChildSessionID,
		Status:         r.Status,
		TurnCount:      r.TurnCount,
		Artifacts:      []string{},
	}
}
