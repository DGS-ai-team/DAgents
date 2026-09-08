package goals

import "time"

type Status string

const (
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusStopped   Status = "stopped"
	StatusCompleted Status = "completed"
	StatusWaiting   Status = "waiting"
	StatusFailed    Status = "failed"
)

type Goal struct {
	ID                     string      `json:"id"`
	Title                  string      `json:"title"`
	Objective              string      `json:"objective"`
	Acceptance             string      `json:"acceptance"`
	AgentID                string      `json:"agent_id"`
	Managed                bool        `json:"managed,omitempty"`
	SessionID              string      `json:"session_id"`
	TriggerID              string      `json:"trigger_id,omitempty"`
	Status                 Status      `json:"status"`
	StatusReason           string      `json:"status_reason,omitempty"`
	MaxRuns                int         `json:"max_runs"`
	Runs                   int         `json:"runs"`
	TokenBudget            int64       `json:"token_budget"`
	TokensUsed             int64       `json:"tokens_used"`
	TurnTokenBudget        int         `json:"turn_token_budget"`
	ExpiresAt              *time.Time  `json:"expires_at,omitempty"`
	NextWakeAt             *time.Time  `json:"next_wake_at,omitempty"`
	MinWakeIntervalSeconds int         `json:"min_wake_interval_seconds"`
	LastCheckpoint         *Checkpoint `json:"last_checkpoint,omitempty"`
	CreatedAt              time.Time   `json:"created_at"`
	UpdatedAt              time.Time   `json:"updated_at"`
}

type Checkpoint struct {
	Summary           string     `json:"summary"`
	Completed         []string   `json:"completed,omitempty"`
	NextSteps         []string   `json:"next_steps,omitempty"`
	Evidence          []string   `json:"evidence,omitempty"`
	Artifacts         []string   `json:"artifacts,omitempty"`
	ExternalCondition string     `json:"external_condition,omitempty"`
	Done              bool       `json:"done"`
	NextWakeAt        *time.Time `json:"next_wake_at,omitempty"`
	At                time.Time  `json:"at"`
}

type Run struct {
	ID         string      `json:"id"`
	GoalID     string      `json:"goal_id"`
	SessionID  string      `json:"session_id"`
	TurnID     string      `json:"turn_id,omitempty"`
	Status     string      `json:"status"`
	Reason     string      `json:"reason,omitempty"`
	Checkpoint *Checkpoint `json:"checkpoint,omitempty"`
	TokensUsed int64       `json:"tokens_used"`
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
}

type CreateInput struct {
	Title                  string     `json:"title"`
	Objective              string     `json:"objective"`
	Acceptance             string     `json:"acceptance"`
	AgentID                string     `json:"agent_id"`
	Managed                bool       `json:"managed,omitempty"`
	SessionID              string     `json:"session_id"`
	MaxRuns                int        `json:"max_runs"`
	TokenBudget            int64      `json:"token_budget"`
	TurnTokenBudget        int        `json:"turn_token_budget"`
	ExpiresAt              *time.Time `json:"expires_at"`
	MinWakeIntervalSeconds int        `json:"min_wake_interval_seconds"`
}
