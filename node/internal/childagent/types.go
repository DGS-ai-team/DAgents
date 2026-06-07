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
	MaxTurns     int
	Wait         bool
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

// ActiveAgent 跟踪单个活跃临时 Agent 的元数据与同步等待（Manager 内存账本，非 session runtime）。
type ActiveAgent struct {
	ChildSessionID  string
	ParentSessionID string
	Purpose         string
	AllowedTools    []string
	Status          Status
	CreatedAt       time.Time
	ExpiresAt       time.Time
	MaxTurns        int
	TurnCount       int
	WaitSync        bool

	mu             sync.Mutex
	terminalResult *Result
	settledCh      chan struct{}
}

func newActiveAgent(parentID string, input CreateInput, childID string, expiresAt time.Time) *ActiveAgent {
	return &ActiveAgent{
		ChildSessionID:  childID,
		ParentSessionID: parentID,
		Purpose:         input.Purpose,
		AllowedTools:    append([]string(nil), input.AllowedTools...),
		Status:          StatusCreating,
		CreatedAt:       time.Now(),
		ExpiresAt:       expiresAt,
		MaxTurns:        input.MaxTurns,
		WaitSync:        input.Wait,
		settledCh:       make(chan struct{}),
	}
}

func (a *ActiveAgent) isTerminal() bool {
	switch a.Status {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusExpired:
		return true
	default:
		return false
	}
}

func (a *ActiveAgent) resultSnapshot() Result {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminalResult != nil {
		out := *a.terminalResult
		return out
	}
	return Result{
		ChildSessionID: a.ChildSessionID,
		Status:         a.Status,
		TurnCount:      a.TurnCount,
		Artifacts:      []string{},
	}
}
