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
	SkillNames   []string
	TTLSeconds   int
	MaxTurns     int
	Wait         bool
}

// Result 为交付给父 Agent 的终态结果。
type Result struct {
	ChildAgentID string   `json:"child_agent_id"`
	Status       Status   `json:"status"`
	Summary      string   `json:"summary"`
	TurnCount    int      `json:"turn_count"`
	Error        string   `json:"error,omitempty"`
	Artifacts    []string `json:"artifacts"`
}

// Progress 是父 Agent 可见的轻量子 Agent 运行快照。它只描述当前阶段
// 与最近一次工具输出摘要，不复制子 Agent 的完整 transcript。
type Progress struct {
	Status            Status    `json:"status"`
	Phase             string    `json:"phase,omitempty"`
	TurnCount         int       `json:"turn_count"`
	MaxTurns          int       `json:"max_turns"`
	CurrentTool       string    `json:"current_tool,omitempty"`
	CurrentToolCallID string    `json:"current_tool_call_id,omitempty"`
	CurrentToolStatus string    `json:"current_tool_status,omitempty"`
	LastOutputPreview string    `json:"last_output_preview,omitempty"`
	PendingApproval   bool      `json:"pending_approval,omitempty"`
	Summary           string    `json:"summary,omitempty"`
	Error             string    `json:"error,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
	Revision          uint64    `json:"revision"`
}

// ActiveAgent 跟踪单个活跃临时 Agent 的元数据与同步等待（Manager 内存账本，非 session runtime）。
type ActiveAgent struct {
	ChildAgentID  string
	ParentAgentID string
	ToolCallID    string
	Purpose       string
	AllowedTools  []string
	LoadedSkills  []string
	Status        Status
	CreatedAt     time.Time
	ExpiresAt     time.Time
	MaxTurns      int
	TurnCount     int
	WaitSync      bool
	Progress      Progress

	mu             sync.Mutex
	terminalResult *Result
	settledCh      chan struct{}
}

func newActiveAgent(parentID string, input CreateInput, childID string, expiresAt time.Time) *ActiveAgent {
	return &ActiveAgent{
		ChildAgentID:  childID,
		ParentAgentID: parentID,
		Purpose:       input.Purpose,
		AllowedTools:  append([]string(nil), input.AllowedTools...),
		LoadedSkills:  append([]string(nil), input.SkillNames...),
		Status:        StatusCreating,
		CreatedAt:     time.Now(),
		ExpiresAt:     expiresAt,
		MaxTurns:      input.MaxTurns,
		WaitSync:      input.Wait,
		Progress: Progress{
			Status:   StatusCreating,
			Phase:    "creating",
			MaxTurns: input.MaxTurns,
		},
		settledCh: make(chan struct{}),
	}
}

// ActiveAgentSnapshot 是跨 package 读取 ActiveAgent 的一致快照，避免 UI
// hydrate 与子 runtime 事件同时到达时直接读取未加锁字段。
type ActiveAgentSnapshot struct {
	ChildAgentID  string
	ParentAgentID string
	ToolCallID    string
	Purpose       string
	AllowedTools  []string
	LoadedSkills  []string
	Status        Status
	CreatedAt     time.Time
	ExpiresAt     time.Time
	MaxTurns      int
	TurnCount     int
	WaitSync      bool
	Progress      Progress
}

func (a *ActiveAgent) Snapshot() ActiveAgentSnapshot {
	if a == nil {
		return ActiveAgentSnapshot{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return ActiveAgentSnapshot{
		ChildAgentID:  a.ChildAgentID,
		ParentAgentID: a.ParentAgentID,
		ToolCallID:    a.ToolCallID,
		Purpose:       a.Purpose,
		AllowedTools:  append([]string(nil), a.AllowedTools...),
		LoadedSkills:  append([]string(nil), a.LoadedSkills...),
		Status:        a.Status,
		CreatedAt:     a.CreatedAt,
		ExpiresAt:     a.ExpiresAt,
		MaxTurns:      a.MaxTurns,
		TurnCount:     a.TurnCount,
		WaitSync:      a.WaitSync,
		Progress:      a.Progress,
	}
}

func (a *ActiveAgent) ProgressSnapshot() Progress {
	if a == nil {
		return Progress{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Progress
}

func (a *ActiveAgent) isTerminal() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.isTerminalLocked()
}

func (a *ActiveAgent) isTerminalLocked() bool {
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
		ChildAgentID: a.ChildAgentID,
		Status:       a.Status,
		TurnCount:    a.TurnCount,
		Artifacts:    []string{},
	}
}
