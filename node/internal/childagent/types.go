// Package childagent 实现临时子 Agent 生命周期、工具与 SSE（见 docs/architecture/child-agent-tools.md）。
package childagent

import (
	"context"
	"sync"
	"time"
)

// Status 为子 Agent 生命周期状态。
type Status string

const (
	StatusCreating    Status = "creating"
	StatusActive      Status = "active"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusCancelled   Status = "cancelled"
	StatusExpired     Status = "expired"
	StatusInterrupted Status = "interrupted"
)

// CreateInput 为 create_temporary_agent 工具入参。
type CreateInput struct {
	Task         string
	Purpose      string
	AllowedTools []string
	SkillNames   []string
	TTLSeconds   int
	MaxTurns     int
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

// ToolActivity 是父 Agent 可见的单条子 Agent 工具活动摘要。
// 它只保留用户理解执行过程所需的关键信息，不复制子 Agent transcript。
type ToolActivity struct {
	ToolCallID    string    `json:"tool_call_id,omitempty"`
	ToolName      string    `json:"tool_name"`
	Status        string    `json:"status"`
	InputSummary  string    `json:"input_summary,omitempty"`
	OutputPreview string    `json:"output_preview,omitempty"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	FinishedAt    time.Time `json:"finished_at,omitempty"`
}

// RunRecord 是生命周期持久化所需的最小控制面记录。具体存储由宿主层
// 注入，childagent 包本身不依赖 SQLite 或 session，避免包循环依赖。
type RunRecord struct {
	ChildAgentID  string
	ParentAgentID string
	NodeID        string
	ToolCallID    string
	Purpose       string
	Status        string
	Phase         string
	AllowedTools  []string
	LoadedSkills  []string
	ProgressJSON  []byte
	TurnCount     int
	MaxTurns      int
	Summary       string
	Error         string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	UpdatedAt     time.Time
	FinishedAt    time.Time
	Revision      uint64
}

// RunRepository 是 ChildRun 的持久化边界。
type RunRepository interface {
	SaveChildRun(context.Context, RunRecord) error
	ListChildRuns(context.Context, string, int) ([]RunRecord, error)
}

// Progress 是父 Agent 可见的轻量子 Agent 运行快照。它描述当前阶段、有限的
// 工具活动摘要与最近一次工具输出，不复制子 Agent 的完整 transcript。
type Progress struct {
	Status              Status         `json:"status"`
	Phase               string         `json:"phase,omitempty"`
	TurnCount           int            `json:"turn_count"`
	MaxTurns            int            `json:"max_turns"`
	CurrentTool         string         `json:"current_tool,omitempty"`
	CurrentToolCallID   string         `json:"current_tool_call_id,omitempty"`
	CurrentToolStatus   string         `json:"current_tool_status,omitempty"`
	LastOutputPreview   string         `json:"last_output_preview,omitempty"`
	RecentTools         []ToolActivity `json:"recent_tools,omitempty"`
	PendingApproval     bool           `json:"pending_approval,omitempty"`
	PendingApprovalData map[string]any `json:"pending_approval_data,omitempty"`
	Summary             string         `json:"summary,omitempty"`
	Error               string         `json:"error,omitempty"`
	UpdatedAt           time.Time      `json:"updated_at"`
	Revision            uint64         `json:"revision"`
	FinishedAt          time.Time      `json:"finished_at,omitempty"`
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
	Progress      Progress
	FinishedAt    time.Time
}

func (a *ActiveAgent) Snapshot() ActiveAgentSnapshot {
	if a == nil {
		return ActiveAgentSnapshot{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	progress := a.Progress
	progress.PendingApprovalData = cloneMap(a.Progress.PendingApprovalData)
	progress.RecentTools = cloneToolActivities(a.Progress.RecentTools)
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
		Progress:      progress,
		FinishedAt:    a.Progress.FinishedAt,
	}
}

func (a *ActiveAgent) ProgressSnapshot() Progress {
	if a == nil {
		return Progress{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	progress := a.Progress
	progress.PendingApprovalData = cloneMap(a.Progress.PendingApprovalData)
	progress.RecentTools = cloneToolActivities(a.Progress.RecentTools)
	return progress
}

func cloneToolActivities(items []ToolActivity) []ToolActivity {
	if len(items) == 0 {
		return nil
	}
	return append([]ToolActivity(nil), items...)
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
	case StatusCompleted, StatusFailed, StatusCancelled, StatusExpired, StatusInterrupted:
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
