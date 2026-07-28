package hooks

import (
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// Context 为 RunPhase 传入的 phase 上下文；按 Phase 仅填充对应 Payload 字段。
type Context struct {
	Phase           Phase
	SessionID       string
	AgentID         string
	TurnID          string
	ParentAgentID string
	Metadata        map[string]any

	MessageEnqueued    *MessageEnqueuedPayload
	TurnBeforeCompress *TurnBeforeCompressPayload
	TurnBeforeStep     *TurnBeforeStepPayload
	PromptBuild        *PromptBuildPayload
	LLMBeforeCall      *LLMCallPayload
	LLMAfterCall       *LLMAfterCallPayload
	ToolBeforeEach     *ToolBeforeEachPayload
	ToolAfterEach      *ToolAfterEachPayload
	// ToolDecision 为 tool.before_each 链的累积决策（RunPhase 链读写）。
	ToolDecision *ToolBeforeEachResult
	// ToolAfterEachOutput 为 tool.after_each 链的结果拆分（RunPhase 链读写）。
	ToolAfterEachOutput *ToolAfterEachOutput
	HITLBeforePause    *HITLBeforePausePayload
	HITLAfterResume    *HITLAfterResumePayload
	TurnDone           *TurnDonePayload
	TurnError          *TurnErrorPayload
	TurnCancel         *TurnCancelPayload
	SessionLifecycle   *SessionLifecyclePayload

	// 以下字段由 EnrichContext 从 Host 快照注入，供 in-process Hook 读取。
	History      []llm.Message          `json:"history,omitempty"`
	SystemPrompt string                 `json:"system_prompt,omitempty"`
	LoadedSkills []LoadedSkillInfo      `json:"loaded_skills,omitempty"`
	Runtime      RuntimeSummary         `json:"runtime"`
	SessionStore map[string]json.RawMessage `json:"session_store,omitempty"`
	FSPaths      *FSPaths               `json:"fs_paths,omitempty"`
}

// MessageEnqueuedPayload 对应 message.enqueued。
type MessageEnqueuedPayload struct {
	Content  string         `json:"content,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// TurnBeforeCompressPayload 对应 turn.before_compress。
type TurnBeforeCompressPayload struct {
	SkipCompress bool `json:"skip_compress,omitempty"`
}

// TurnBeforeStepPayload 对应 turn.before_step。
type TurnBeforeStepPayload struct {
	StepKind string `json:"step_kind,omitempty"`
}

// PromptBuildPayload 对应 prompt.build。
type PromptBuildPayload struct {
	SystemPrompt string `json:"system_prompt,omitempty"`
}

// LLMCallPayload 对应 llm.before_call / 部分 llm.after_call 输入。
type LLMCallPayload struct {
	Messages []llm.Message `json:"messages,omitempty"`
}

// LLMAfterCallPayload 对应 llm.after_call。
type LLMAfterCallPayload struct {
	AssistantContent string         `json:"assistant_content,omitempty"`
	ToolCalls        []llm.ToolCall `json:"tool_calls,omitempty"`
}

// ToolBeforeEachPayload 对应 tool.before_each（通用 phase 视图）。
type ToolBeforeEachPayload struct {
	ToolName     string         `json:"tool_name,omitempty"`
	ToolArgs     map[string]any `json:"tool_args,omitempty"`
	RawArguments string         `json:"raw_arguments,omitempty"`
}

// ToolAfterEachPayload 对应 tool.after_each（通用 phase 视图）。
type ToolAfterEachPayload struct {
	ToolCallID   string         `json:"tool_call_id,omitempty"`
	ToolName     string         `json:"tool_name,omitempty"`
	ToolArgs     map[string]any `json:"tool_args,omitempty"`
	RawArguments string         `json:"raw_arguments,omitempty"`
	RawResult    string         `json:"raw_result,omitempty"`
}

// HITLBeforePausePayload 对应 hitl.before_pause。
type HITLBeforePausePayload struct {
	Reason string `json:"reason,omitempty"`
}

// HITLAfterResumePayload 对应 hitl.after_resume。
type HITLAfterResumePayload struct {
	ResumeKind string `json:"resume_kind,omitempty"`
}

// TurnDonePayload 对应 turn.done。
type TurnDonePayload struct {
	FinishReason string `json:"finish_reason,omitempty"`
}

// TurnErrorPayload 对应 turn.error。
type TurnErrorPayload struct {
	Err error `json:"-"`
}

// TurnCancelPayload 对应 turn.cancel。
type TurnCancelPayload struct {
	Reason string `json:"reason,omitempty"`
}

// SessionLifecyclePayload 对应 session.lifecycle。
type SessionLifecyclePayload struct {
	Event string `json:"event,omitempty"` // create | destroy
}
