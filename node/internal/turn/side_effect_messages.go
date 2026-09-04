package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
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

// BuildAsyncSideEffectMessages 预构建异步工具结果的 Produce/Apply 消息 bundle。
func (o *Orchestrator) BuildAsyncSideEffectMessages(
	sessionID string,
	history []llm.Message,
	async queue.AsyncToolResultPayload,
) SideEffectMessages {
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
}

func sideEffectFromAsync(b asyncToolMessages) SideEffectMessages {
	return SideEffectMessages(b)
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
func ResolveSideEffectInsertSite(messages []llm.Message) SideEffectInsertSite {
	if len(messages) == 0 {
		return SideEffectInsertSite{InsertAt: 0, Mode: "empty_history_bridge", Continue: true}
	}
	tail := classifyToolResultTail(messages)
	site := SideEffectInsertSite{Continue: shouldContinueAfterAsyncTool(tail)}
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
			Messages: []llm.Message{bridgeApplyUserMessage(built)},
			Continue: true,
			Mode:     "empty_history_bridge",
		}
	}
	site := ResolveSideEffectInsertSite(messages)
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
		return []llm.Message{bridgeApplyUserMessage(built)}
	default:
		return []llm.Message{built.AssistantMessage, built.ToolMessage}
	}
}

// bridgeApplyUserMessage 桥接态 Apply：单条合成 user（合并原 user 提示与 tool 正文）。
func bridgeApplyUserMessage(built SideEffectMessages) llm.Message {
	message := built.UserMessage
	if strings.TrimSpace(message.Name) == "" {
		message = llm.UserMessageWithSource(
			message.Content,
			llm.UserNameAsyncTool,
			llm.MessageSource{Kind: llm.MessageSourceAsyncTool, Form: llm.MessageFormRelay},
			&llm.MessageProvenance{Producer: llm.UserNameAsyncTool, Operation: "bridge"},
		)
	}
	content := mergeBridgeUserContent(built)
	message.Content = content
	message.ContentParts = nil
	return message
}

func mergeBridgeUserContent(built SideEffectMessages) string {
	user := strings.TrimSpace(built.UserMessage.Content)
	tool := strings.TrimSpace(built.ToolMessage.Content)
	switch {
	case user == "":
		return tool
	case tool == "":
		return user
	default:
		return user + "\n\n" + tool
	}
}

func isSideEffectBridgeUserMessage(message llm.Message) bool {
	source := llm.EffectiveMessageSource(message)
	switch source.Kind {
	case llm.MessageSourceAsyncTool, llm.MessageSourceTrigger, llm.MessageSourceChildAgent:
		return true
	default:
		return false
	}
}

func isSideEffectBridgeUserTail(messages []llm.Message) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	return last.Role == "user" && isSideEffectBridgeUserMessage(last)
}

type mergedCallbackItem struct {
	Kind     string `json:"kind"`
	JobID    string `json:"job_id,omitempty"`
	ToolName string `json:"tool_name,omitempty"`
	Status   string `json:"status,omitempty"`
	Content  string `json:"content"`
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
			Kind:    "async",
			Content: e.Built.ForClientContent,
			Status:  e.Built.Status,
		}
		item.JobID = strings.TrimSpace(e.Async.JobID)
		item.ToolName = strings.TrimSpace(e.Async.ToolName)
		item.Status = strings.TrimSpace(e.Async.Status)
		if item.Content == "" {
			item.Content = strings.TrimSpace(e.Async.ResultText)
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
	toolMsg := llm.ToolResultMessage(toolCallID, "get_callback", string(body))
	out := SideEffectApplyPlan{
		Continue: shouldContinueAfterAsyncTool(tail),
		Mode:     "merged_get_callback",
	}
	if tail == tailAssistantWithoutToolCalls {
		out.Messages = []llm.Message{
			llm.UserMessage(formatMergedBridgeUserMessage(items), llm.UserNameAsyncTool),
		}
	} else {
		out.Messages = []llm.Message{assistant, toolMsg}
	}
	return out
}

func formatMergedBridgeUserMessage(items []mergedCallbackItem) string {
	body, _ := json.Marshal(map[string]any{"callbacks": items})
	return "似乎已经有回调事件了，请查看并继续任务。\n\n" + string(body)
}

// SideEffectBatchEntry Apply 批量收集条目。
type SideEffectBatchEntry struct {
	Built SideEffectMessages
	Async queue.AsyncToolResultPayload
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

func shouldContinueAfterSideEffectApplyMessages(messages []llm.Message) bool {
	if shouldContinueAfterAsyncTool(classifyToolResultTail(messages)) {
		return true
	}
	return isSideEffectBridgeUserTail(messages)
}

// ShouldContinueAfterSideEffectApply 供 runtime reconcile 判定。
func ShouldContinueAfterSideEffectApply(messages []llm.Message) bool {
	return shouldContinueAfterSideEffectApplyMessages(messages)
}

// SideEffectAlreadyApplied 根据 async job_id 判断结果是否已经写入 history。
func SideEffectAlreadyApplied(messages []llm.Message, async queue.AsyncToolResultPayload) bool {
	jobID := strings.TrimSpace(async.JobID)
	if jobID == "" {
		return false
	}
	for _, m := range messages {
		if asyncToolResultAppliedInHistory(m.Content, jobID) {
			return true
		}
	}
	return false
}

// ContinueAfterSideEffects side_effect_continue handler 续跑 LLM。
func (o *Orchestrator) ContinueAfterSideEffects(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
) StepOutcome {
	stepIndex := StepIndexFromContext(ctx)
	if !shouldContinueAfterSideEffectApplyMessages(*history) {
		return StepOutcome{StepIndex: stepIndex}
	}
	tail := classifyToolResultTail(*history)
	if tail == tailTool {
		return o.RunToolMessageTurn(ctx, sessionID, history)
	}
	o.logger.Info("turn side effect bridge continue",
		"session_id", sessionID,
		"user_name", (*history)[len(*history)-1].Name,
	)
	return o.runOneStep(ctx, sessionID, history)
}
