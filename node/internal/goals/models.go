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
	// Cycle metadata is zero-valued for legacy non-managed goals.
	CycleSequence    int    `json:"cycle_sequence,omitempty"`
	ProfileRevision  int64  `json:"profile_revision,omitempty"`
	ConfigRevision   int64  `json:"config_revision,omitempty"`
	Revision         int64  `json:"revision,omitempty"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
	CycleFingerprint string `json:"cycle_fingerprint,omitempty"`
	ProvisionStatus  string `json:"provision_status,omitempty"`
	EnableIntent     bool   `json:"enable_intent,omitempty"`
	PreviousGoalID   string `json:"previous_goal_id,omitempty"`
	Origin           string `json:"origin,omitempty"`
}

// AutoProfile is the durable Agent-owned identity for autonomous work.
// CurrentGoalID is the only binding to a live business cycle.
type AutoProfile struct {
	AgentID                string    `json:"agent_id"`
	Revision               int64     `json:"revision"`
	Enabled                bool      `json:"enabled"`
	RoleObjective          string    `json:"role_objective"`
	RoleBoundaries         string    `json:"role_boundaries"`
	PlanMode               string    `json:"plan_mode"`
	Timezone               string    `json:"timezone"`
	WorkSchedule           string    `json:"work_schedule"`
	CurrentGoalID          string    `json:"current_goal_id,omitempty"`
	AuthorizationRef       string    `json:"authorization_ref,omitempty"`
	AuthorizationRevision  int64     `json:"authorization_revision,omitempty"`
	BusinessTokenBudget    int64     `json:"business_token_budget,omitempty"`
	MaintenanceTokenBudget int64     `json:"maintenance_token_budget,omitempty"`
	TotalTokenBudget       int64     `json:"total_token_budget,omitempty"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// AgentUsage is Agent-scoped; a new cycle cannot reset accumulated usage.
type AgentUsage struct {
	AgentID           string    `json:"agent_id"`
	BusinessTokens    int64     `json:"business_tokens"`
	MaintenanceTokens int64     `json:"maintenance_tokens"`
	UnknownTokens     int64     `json:"unknown_tokens"`
	Unknown           bool      `json:"unknown"`
	UnknownReason     string    `json:"unknown_reason,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type UsageReceipt struct {
	AgentID     string `json:"agent_id"`
	Fingerprint string `json:"fingerprint"`
}

type MigrationIssue struct {
	AgentID   string    `json:"agent_id"`
	Reason    string    `json:"reason"`
	GoalIDs   []string  `json:"goal_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Checkpoint struct {
	Summary           string         `json:"summary"`
	Completed         []string       `json:"completed,omitempty"`
	NextSteps         []string       `json:"next_steps,omitempty"`
	Evidence          []string       `json:"evidence,omitempty"`
	Artifacts         []string       `json:"artifacts,omitempty"`
	ExternalCondition string         `json:"external_condition,omitempty"`
	Done              bool           `json:"done"`
	NextWakeAt        *time.Time     `json:"next_wake_at,omitempty"`
	At                time.Time      `json:"at"`
	Decision          *FinalDecision `json:"decision,omitempty"`
}

type Run struct {
	ID              string      `json:"id"`
	GoalID          string      `json:"goal_id"`
	SessionID       string      `json:"session_id"`
	TurnID          string      `json:"turn_id,omitempty"`
	Status          string      `json:"status"`
	Reason          string      `json:"reason,omitempty"`
	Checkpoint      *Checkpoint `json:"checkpoint,omitempty"`
	TokensUsed      int64       `json:"tokens_used"`
	StartedAt       time.Time   `json:"started_at"`
	FinishedAt      *time.Time  `json:"finished_at,omitempty"`
	GoalRevision    int64       `json:"goal_revision,omitempty"`
	ProfileRevision int64       `json:"profile_revision,omitempty"`
	ConfigRevision  int64       `json:"config_revision,omitempty"`
	Generation      int64       `json:"generation,omitempty"`
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
	EnabledIntent          bool       `json:"-"`
}
