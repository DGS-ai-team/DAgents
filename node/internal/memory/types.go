// Package memory 提供长期记忆的存储、召回和冲突处理领域模型。
//
// 该包不依赖 session 或 turn。Turn 只负责在正确的生命周期边界调用
// Service，并把返回的 Snapshot 渲染成 request-only 上下文。
package memory

import (
	"context"
	"time"
)

type Scope string

const (
	ScopeAgent  Scope = "agent"
	ScopeGlobal Scope = "global"
)

type Tier string

const (
	TierCore   Tier = "core"
	TierRecall Tier = "recall"
)

type Kind string

const (
	KindFact       Kind = "fact"
	KindPreference Kind = "preference"
	KindDecision   Kind = "decision"
	KindProcedure  Kind = "procedure"
	KindExperience Kind = "experience"
	KindConstraint Kind = "constraint"
)

type Status string

const (
	StatusActive     Status = "active"
	StatusPending    Status = "pending"
	StatusSuperseded Status = "superseded"
	StatusConflicted Status = "conflicted"
	StatusDeleted    Status = "deleted"
)

// Entry 是一条结构化长期记忆。结构化字段允许 Repository 先做确定性
// 去重/冲突判断，content 仍然保留模型可读的原始正文。
type Entry struct {
	ID             string            `json:"id"`
	Scope          Scope             `json:"scope"`
	Tier           Tier              `json:"tier"`
	Kind           Kind              `json:"kind"`
	SemanticKey    string            `json:"semantic_key,omitempty"`
	Subject        string            `json:"subject,omitempty"`
	Predicate      string            `json:"predicate,omitempty"`
	Value          any               `json:"value,omitempty"`
	Qualifiers     map[string]string `json:"qualifiers,omitempty"`
	Cardinality    string            `json:"cardinality,omitempty"`
	Content        string            `json:"content"`
	NormalizedText string            `json:"normalized_content,omitempty"`
	ContentHash    string            `json:"content_hash,omitempty"`
	Status         Status            `json:"status"`
	Importance     int               `json:"importance"`
	Confidence     int               `json:"confidence"`
	Sensitivity    string            `json:"sensitivity,omitempty"`
	SourceType     string            `json:"source_type"`
	SourceSession  string            `json:"source_session_id,omitempty"`
	SourceMessage  string            `json:"source_message_id,omitempty"`
	SourceRef      string            `json:"source_reference,omitempty"`
	SupersedesID   string            `json:"supersedes_id,omitempty"`
	ConflictGroup  string            `json:"conflict_group_id,omitempty"`
	ValidFrom      *time.Time        `json:"valid_from,omitempty"`
	ValidTo        *time.Time        `json:"valid_to,omitempty"`
	ExpiresAt      *time.Time        `json:"expires_at,omitempty"`
	Revision       int64             `json:"revision"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	LastAccessedAt *time.Time        `json:"last_accessed_at,omitempty"`
	AccessCount    int64             `json:"access_count"`
}

type Reference struct {
	ID        string    `json:"id"`
	Revision  int64     `json:"revision"`
	Tier      Tier      `json:"tier"`
	Kind      Kind      `json:"kind"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Snapshot 是一个 Turn 使用的不可变记忆视图。RenderedContent 需要一起
// 冻结，避免审批恢复或 Node 重启后依据已变化的数据库重新召回出不同上下文。
type Snapshot struct {
	ID              string      `json:"id"`
	Scope           Scope       `json:"scope"`
	StoreRevision   int64       `json:"store_revision"`
	RootMessageID   string      `json:"root_message_id"`
	Core            []Reference `json:"core,omitempty"`
	Recalled        []Reference `json:"recalled,omitempty"`
	RenderedContent string      `json:"rendered_content"`
	TokenEstimate   int         `json:"token_estimate"`
	Digest          string      `json:"digest"`
	CreatedAt       time.Time   `json:"created_at"`
}

type RecallRequest struct {
	AgentID       string
	Scope         Scope
	RootMessageID string
	QueryText     string
	Limit         int
	TokenBudget   int
	CoreBudget    int
	// ContextTokenBudget is the hard budget for the complete rendered
	// memory_context, including its framing and metadata. TokenBudget and
	// CoreBudget remain the per-tier selection budgets.
	ContextTokenBudget int
	IncludeCore        bool
	Now                time.Time
}

type SearchRequest struct {
	Scope           Scope
	Query           string
	Limit           int
	IncludeInactive bool
	Now             time.Time
}

type RememberRequest struct {
	Scope         Scope
	AgentID       string
	Information   string
	Kind          Kind
	Tier          Tier
	SemanticKey   string
	Subject       string
	Predicate     string
	Value         any
	Qualifiers    map[string]string
	Cardinality   string
	Importance    int
	Confidence    int
	Sensitivity   string
	SourceType    string
	SourceSession string
	SourceMessage string
	SourceRef     string
	ValidFrom     *time.Time
	ValidTo       *time.Time
	ExpiresAt     *time.Time
}

// ExtractionMessage is the provider-neutral representation of one message
// passed to a background candidate extractor. It intentionally contains only
// the frozen compression slice; it is not a session message and must never be
// written back to a Turn.
type ExtractionMessage struct {
	Role       string               `json:"role"`
	Name       string               `json:"name,omitempty"`
	Content    string               `json:"content,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	ToolCalls  []ExtractionToolCall `json:"tool_calls,omitempty"`
}

type ExtractionToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

// ExtractionInput is a frozen, bounded source for background memory
// extraction. SourceFingerprint ties any candidate back to the exact
// compression slice without storing the slice in the memory database.
type ExtractionInput struct {
	AgentID           string              `json:"agent_id"`
	SessionID         string              `json:"session_id"`
	Scope             Scope               `json:"scope,omitempty"`
	SourceFingerprint string              `json:"source_fingerprint"`
	Messages          []ExtractionMessage `json:"messages"`
}

// Candidate is model-inferred information awaiting serial consolidation. A
// candidate is never directly visible to Recall; the consolidator first
// stages it as pending and only promotes conflict-free candidates.
type Candidate struct {
	Request RememberRequest `json:"request"`
}

// CandidateExtractor is an optional extension point. Implementations must be
// cancellation-aware and must not mutate the input slice.
type CandidateExtractor interface {
	Extract(context.Context, ExtractionInput) ([]Candidate, error)
}

// Consolidator serializes candidate writes for one Agent. It must never
// approve a conflict automatically; conflicting candidates remain pending.
type Consolidator interface {
	Consolidate(context.Context, []Candidate) ([]WriteResult, error)
}

type Maintainer interface {
	Maintain(context.Context, Scope, int) (MaintenanceReport, error)
}

// CandidateSubmitter is the small boundary used by compression. Keeping this
// interface separate means compression does not know about queues, stores or
// Turn lifecycle.
type CandidateSubmitter interface {
	Submit(ExtractionInput) bool
}

type ConsolidationReport struct {
	CandidateCount   int   `json:"candidate_count"`
	Changed          bool  `json:"changed"`
	Added            int   `json:"added"`
	Duplicates       int   `json:"duplicates"`
	PendingConflicts int   `json:"pending_conflicts"`
	Superseded       int   `json:"superseded"`
	StoreRevision    int64 `json:"store_revision"`
}

type MaintenanceReport struct {
	Scope          Scope `json:"scope"`
	CoreBudget     int   `json:"core_budget"`
	DemotedCore    int   `json:"demoted_core"`
	ExpiredEntries int   `json:"expired_entries"`
	Changed        bool  `json:"changed"`
	StoreRevision  int64 `json:"store_revision"`
}

type WriteOutcome string

const (
	WriteAdded           WriteOutcome = "added"
	WriteDuplicate       WriteOutcome = "duplicate"
	WriteSuperseded      WriteOutcome = "superseded"
	WritePendingConflict WriteOutcome = "pending_conflict"
)

type ConflictDecision string

const (
	ConflictKeepOld  ConflictDecision = "keep_old"
	ConflictUseNew   ConflictDecision = "use_new"
	ConflictKeepBoth ConflictDecision = "keep_both"
	ConflictCancel   ConflictDecision = "cancel"
)

type Conflict struct {
	ID                string    `json:"id"`
	Candidate         Entry     `json:"candidate"`
	Existing          []Entry   `json:"existing"`
	ExistingRevisions []int64   `json:"existing_revisions"`
	Relation          string    `json:"relation"`
	Description       string    `json:"description"`
	StoreRevision     int64     `json:"store_revision"`
	CreatedAt         time.Time `json:"created_at"`
}

type WriteResult struct {
	Outcome       WriteOutcome `json:"outcome"`
	Entry         *Entry       `json:"entry,omitempty"`
	ExistingID    string       `json:"existing_id,omitempty"`
	Superseded    []string     `json:"superseded,omitempty"`
	Conflict      *Conflict    `json:"conflict,omitempty"`
	StoreRevision int64        `json:"store_revision"`
}

type SearchResult struct {
	Entry Entry   `json:"entry"`
	Score float64 `json:"score,omitempty"`
}

// Service 是 Turn/API 依赖的最小能力集合。具体实现可以是 Workspace
// SQLite，也可以是迁移期间的 legacy adapter。
type Service interface {
	Recall(ctx context.Context, req RecallRequest) (Snapshot, error)
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)
	Get(ctx context.Context, scope Scope, id string, includeInactive bool) (Entry, error)
	Remember(ctx context.Context, req RememberRequest) (WriteResult, error)
	Forget(ctx context.Context, scope Scope, id, reason string) (WriteResult, error)
	ResolveConflict(ctx context.Context, scope Scope, conflictID string, decision ConflictDecision) (WriteResult, error)
	SetScope(scope Scope)
}
