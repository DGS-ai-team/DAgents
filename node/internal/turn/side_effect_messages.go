package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
)

// SideEffectKind 旁路回灌类型。
type SideEffectKind string

const (
	SideEffectAsync           SideEffectKind = "async"
	SideEffectExternalMessage SideEffectKind = "external_message"
)

// SideEffectMessages 旁路写回 history / SSE 的消息 bundle。
type SideEffectMessages struct {
	UserMessage            llm.Message
	AssistantMessage       llm.Message
	ToolMessage            llm.Message
	ForClientContent       string
	ToolName               string
	ToolCallID             string
	Status                 string
	OutputCompressSavedPct int
	OutputCompressRawRunes int
	OutputCompressOutRunes int
}

// SideEffectInsertSite Apply 插入点。
type SideEffectInsertSite struct {
	Ready    bool
	InsertAt int
	Mode     string
	Continue bool
}

// SideEffectApplyPlan 单条或合并 batch 的 Apply 计划。
type SideEffectApplyPlan struct {
	Messages []llm.Message
	Continue bool
	Mode     string
}

// BuildSideEffectMessages 预构建 Produce/Apply 用的消息 bundle。
func (o *Orchestrator) BuildSideEffectMessages(
	kind SideEffectKind,
	sessionID string,
	history []llm.Message,
	async queue.AsyncToolResultPayload,
	content, userName string,
) SideEffectMessages {
	switch kind {
	case SideEffectAsync:
		built := o.buildAsyncToolMessages(sessionID, history, AsyncToolResultInput{
			JobID:                  async.JobID,
			ToolName:               async.ToolName,
			ToolCallID:             async.ToolCallID,
			Status:                 async.Status,
			ResultText:             async.ResultText,
			ErrorText:              async.ErrorText,
			OutputCompressSavedPct: async.OutputCompressSavedPct,
			OutputCompressRawRunes: async.OutputCompressRawRunes,
			OutputCompressOutRunes: async.OutputCompressOutRunes,
		})
		return sideEffectFromAsync(built)
	default:
		return o.buildExternalSideEffectMessages(sessionID, content, userName)
	}
}

func sideEffectFromAsync(b asyncToolMessages) SideEffectMessages {
	return SideEffectMessages{
		UserMessage:            b.UserMessage,
		AssistantMessage:       b.AssistantMessage,
		ToolMessage:            b.ToolMessage,
		ForClientContent:       b.ForClientContent,
		ToolName:               b.ToolName,
		ToolCallID:             b.ToolCallID,
		Status:                 b.Status,
		OutputCompressSavedPct: b.OutputCompressSavedPct,
		OutputCompressRawRunes: b.OutputCompressRawRunes,
		OutputCompressOutRunes: b.OutputCompressOutRunes,
	}
}

func (o *Orchestrator) buildExternalSideEffectMessages(sessionID, content, userName string) SideEffectMessages {
	content = strings.TrimSpace(content)
	userName = llm.NormalizeUserMessageName(userName)
	toolCallID := fmt.Sprintf("external-%s", shortHash(content+userName))
	toolName := "tool_callback"
	argsJSON := fmt.Sprintf(`{"source":%q,"user_name":%q}`, userName, userName)
	toolText := content
	if toolText == "" {
		toolText = "(empty external message)"
	}
	return SideEffectMessages{
		UserMessage: llm.UserMessage(content, userName),
		AssistantMessage: llm.Message{
			Role:    "assistant",
			Content: "",
			ToolCalls: []llm.ToolCall{{
				ID:   toolCallID,
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      toolName,
					Arguments: argsJSON,
				},
			}},
		},
		ToolMessage: llm.Message{
			Role:       "tool",
			ToolCallID: toolCallID,
			Content:    fmt.Sprintf("外部消息（%s）：%s", userName, toolText),
		},
		ForClientContent: toolText,
		ToolName:         toolName,
		ToolCallID:       toolCallID,
		Status:           "delivered",
	}
}

func shortHash(s string) string {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%08x", h)
}

// ResolveSideEffectInsertSite 按 tail 解析单条插入点（kind 无关）。
func ResolveSideEffectInsertSite(messages []llm.Message, built SideEffectMessages) SideEffectInsertSite {
	if len(messages) == 0 {
		return SideEffectInsertSite{Ready: true, InsertAt: 0, Mode: "empty_history_bridge", Continue: true}
	}
	_ = built
	tail := classifyToolResultTail(messages)
	site := SideEffectInsertSite{Ready: true, Continue: shouldContinueAfterSideEffectApply(tail)}
	switch tail {
	case tailTool:
		site.InsertAt = len(messages)
		site.Mode = "append_callback"
	case tailAssistantWithToolCalls:
		site.InsertAt = len(messages) - 1
		site.Mode = "insert_before_last_assistant"
	case tailAssistantWithoutToolCalls:
		site.InsertAt = len(messages)
		site.Mode = "bridge_user_callback"
		site.Continue = true
	default:
		site.InsertAt = len(messages)
		site.Mode = "append_callback_fallback"
	}
	return site
}

// PlanSingleSideEffectApply 单条 Apply 计划。
func PlanSingleSideEffectApply(messages []llm.Message, built SideEffectMessages) SideEffectApplyPlan {
	if len(messages) == 0 {
		return SideEffectApplyPlan{
			Messages: []llm.Message{built.UserMessage, built.AssistantMessage, built.ToolMessage},
			Continue: true,
			Mode:     "empty_history_bridge",
		}
	}
	site := ResolveSideEffectInsertSite(messages, built)
	if !site.Ready {
		return SideEffectApplyPlan{}
	}
	tail := classifyToolResultTail(messages)
	msgs := selectSideEffectSegments(built, tail)
	return SideEffectApplyPlan{
		Messages: msgs,
		Continue: site.Continue,
		Mode:     site.Mode,
	}
}

func selectSideEffectSegments(built SideEffectMessages, tail toolResultTailKind) []llm.Message {
	switch tail {
	case tailAssistantWithoutToolCalls:
		return []llm.Message{built.UserMessage, built.AssistantMessage, built.ToolMessage}
	default:
		return []llm.Message{built.AssistantMessage, built.ToolMessage}
	}
}

type mergedCallbackItem struct {
	Kind      string `json:"kind"`
	JobID     string `json:"job_id,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	Status    string `json:"status,omitempty"`
	TriggerID string `json:"trigger_id,omitempty"`
	UserName  string `json:"user_name,omitempty"`
	Content   string `json:"content"`
}

// BuildMergedCallbackBatch 多条合并为 get_callback 批次。
func BuildMergedCallbackBatch(entries []SideEffectBatchEntry, messages []llm.Message) SideEffectApplyPlan {
	if len(entries) < 2 {
		return SideEffectApplyPlan{}
	}
	tail := classifyToolResultTail(messages)
	items := make([]mergedCallbackItem, 0, len(entries))
	for _, e := range entries {
		item := mergedCallbackItem{
			Kind:     string(e.Kind),
			Content:  e.Built.ForClientContent,
			UserName: e.UserName,
			Status:   e.Built.Status,
		}
		if e.Kind == SideEffectAsync {
			item.JobID = strings.TrimSpace(e.Async.JobID)
			item.ToolName = strings.TrimSpace(e.Async.ToolName)
			item.Status = strings.TrimSpace(e.Async.Status)
			if item.Content == "" {
				item.Content = strings.TrimSpace(e.Async.ResultText)
			}
		}
		if e.TriggerID != "" {
			item.TriggerID = e.TriggerID
		}
		if item.Content == "" {
			item.Content = strings.TrimSpace(e.MessageContent)
		}
		items = append(items, item)
	}
	body, _ := json.Marshal(map[string]any{"callbacks": items})
	toolCallID := fmt.Sprintf("get-callback-batch-%s", shortHash(string(body)))
	assistant := llm.Message{
		Role:    "assistant",
		Content: "获取当前回调事件",
		ToolCalls: []llm.ToolCall{{
			ID:   toolCallID,
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "get_callback",
				Arguments: "{}",
			},
		}},
	}
	toolMsg := llm.Message{
		Role:       "tool",
		ToolCallID: toolCallID,
		Content:    string(body),
	}
	out := SideEffectApplyPlan{
		Continue: shouldContinueAfterSideEffectApply(tail),
		Mode:     "merged_get_callback",
	}
	if tail == tailAssistantWithoutToolCalls {
		out.Messages = []llm.Message{
			llm.UserMessage("似乎已经有回调事件了，请查看", llm.UserNameAsyncTool),
			assistant,
			toolMsg,
		}
	} else {
		out.Messages = []llm.Message{assistant, toolMsg}
	}
	return out
}

// SideEffectBatchEntry Apply 批量收集条目。
type SideEffectBatchEntry struct {
	Kind           SideEffectKind
	Built          SideEffectMessages
	Async          queue.AsyncToolResultPayload
	MessageContent string
	UserName       string
	TriggerID      string
}

// ApplySideEffectPlan 按 site/plan 写入 history（不发 SSE）。
func (o *Orchestrator) ApplySideEffectPlan(sessionID string, history *[]llm.Message, site SideEffectInsertSite, plan SideEffectApplyPlan) {
	if len(plan.Messages) == 0 {
		return
	}
	insertAt := site.InsertAt
	if insertAt < 0 {
		insertAt = len(*history)
	}
	for i, msg := range plan.Messages {
		o.insertHistory(sessionID, history, insertAt+i, msg)
	}
}

func shouldContinueAfterSideEffectApply(tail toolResultTailKind) bool {
	return shouldContinueAfterAsyncTool(tail)
}

// ShouldContinueAfterSideEffectApply 供 runtime reconcile 判定。
func ShouldContinueAfterSideEffectApply(messages []llm.Message) bool {
	return shouldContinueAfterSideEffectApply(classifyToolResultTail(messages))
}

// SideEffectAlreadyApplied 幂等：async job_id 或 external 内容已在 history。
func SideEffectAlreadyApplied(messages []llm.Message, kind SideEffectKind, async queue.AsyncToolResultPayload, content, userName string) bool {
	jobID := strings.TrimSpace(async.JobID)
	if kind == SideEffectAsync && jobID != "" {
		for _, m := range messages {
			if m.Role == "tool" && asyncToolResultAppliedInHistory(m.Content, jobID) {
				return true
			}
		}
		return false
	}
	content = strings.TrimSpace(content)
	userName = llm.NormalizeUserMessageName(userName)
	if content == "" {
		return false
	}
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	if last.Role == "user" && last.Content == content && last.Name == userName {
		return true
	}
	return false
}

// ContinueAfterSideEffects side_effect_continue handler 续跑 LLM。
func (o *Orchestrator) ContinueAfterSideEffects(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	setState StateSetter,
	toolLoopCount int,
) StepOutcome {
	if setState == nil {
		setState = func(State) {}
	}
	tail := classifyToolResultTail(*history)
	if shouldContinueAfterSideEffectApply(tail) {
		return o.RunToolMessageTurn(ctx, sessionID, history, setState, toolLoopCount)
	}
	return StepOutcome{LoopCount: toolLoopCount}
}
