package turn

import (
	"context"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type toolResultTailKind string

const (
	tailTool                      toolResultTailKind = "tail_tool"
	tailAssistantWithToolCalls    toolResultTailKind = "tail_assistant_with_tool_calls"
	tailAssistantWithoutToolCalls toolResultTailKind = "tail_assistant_without_tool_calls"
	tailOther                     toolResultTailKind = "other"

	// TailAssistantWithoutToolCalls 供 session 等外部包判定桥接态。
	TailAssistantWithoutToolCalls = tailAssistantWithoutToolCalls
)

// ClassifyToolResultTail 导出尾部形态判定。
func ClassifyToolResultTail(messages []llm.Message) toolResultTailKind {
	return classifyToolResultTail(messages)
}

// IsBridgeTail 任务已完成桥接态（尾部纯 assistant）。
func IsBridgeTail(messages []llm.Message) bool {
	return classifyToolResultTail(messages) == tailAssistantWithoutToolCalls
}

func classifyToolResultTail(messages []llm.Message) toolResultTailKind {
	if len(messages) == 0 {
		return tailOther
	}
	last := messages[len(messages)-1]
	switch last.Role {
	case "tool":
		return tailTool
	case "assistant":
		if len(last.ToolCalls) > 0 {
			return tailAssistantWithToolCalls
		}
		return tailAssistantWithoutToolCalls
	default:
		return tailOther
	}
}

// asyncToolMessages 为 async_tool_result 写回 history 的三段消息。
type asyncToolMessages struct {
	UserMessage            llm.Message
	AssistantMessage       llm.Message
	ToolMessage            llm.Message
	ForClientContent       string // SSE 展示用全文（清洗后、未 history 摘要）
	ToolName               string
	ToolCallID             string
	Status                 string
	OutputCompressSavedPct int
	OutputCompressRawRunes int
	OutputCompressOutRunes int
}

func (o *Orchestrator) buildAsyncToolMessages(sessionID string, history []llm.Message, payload AsyncToolResultInput) asyncToolMessages {
	toolName := strings.TrimSpace(payload.ToolName)
	if toolName == "" {
		toolName = "unknown_tool"
	}
	jobID := strings.TrimSpace(payload.JobID)
	if jobID == "" {
		jobID = "unknown-job"
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = "succeeded"
	}
	src := lookupAsyncSourceFromHistory(history, toolName, jobID, payload.ToolCallID)
	toolCallID := asyncCallbackToolCallID(jobID)
	resultBody := strings.TrimSpace(payload.ResultText)
	if status != "succeeded" {
		if errText := strings.TrimSpace(payload.ErrorText); errText != "" {
			resultBody = errText
		} else if resultBody == "" {
			resultBody = "工具执行失败"
		}
	}
	fullForClient := resultBody
	historyBody := resultBody
	if o.toolHooks != nil {
		hc := hooks.BuildToolAfterEachContext(hooks.ToolAfterEachInput{
			SessionID:  sessionID,
			ToolCallID: toolCallID,
			ToolName:   toolName,
			RawResult:  resultBody,
		})
		out, err := o.toolHooks.RunPhase(context.Background(), hooks.PhaseToolAfterEach, hc)
		if err == nil {
			split := hooks.ToolAfterEachOutputFrom(out)
			fullForClient = split.ForClient
			historyBody = split.ForHistory
		}
	}
	toolText := formatAsyncToolResultContent(toolName, jobID, status, src, historyBody)
	userText := formatAsyncToolUserMessage(toolName, jobID, status, src.CallPurpose)
	argsJSON := formatAsyncToolCallbackArgs(toolName, jobID, status, src)
	_ = fullForClient // referenced via ForClientContent
	return asyncToolMessages{
		UserMessage: llm.UserMessage(userText, llm.UserNameAsyncTool),
		AssistantMessage: llm.Message{
			Role:    "assistant",
			Content: "",
			ToolCalls: []llm.ToolCall{{
				ID:   toolCallID,
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "tool_callback",
					Arguments: argsJSON,
				},
			}},
		},
		ToolMessage: llm.Message{
			Role:       "tool",
			ToolCallID: toolCallID,
			Content:    toolText,
		},
		ForClientContent:       fullForClient,
		ToolName:               toolName,
		ToolCallID:             toolCallID,
		Status:                 status,
		OutputCompressSavedPct: payload.OutputCompressSavedPct,
		OutputCompressRawRunes: payload.OutputCompressRawRunes,
		OutputCompressOutRunes: payload.OutputCompressOutRunes,
	}
}

// AsyncToolResultInput 为 async 旁路 side-effect 消息构建入参。
type AsyncToolResultInput struct {
	JobID                  string
	ToolName               string
	ToolCallID             string
	Status                 string
	ResultText             string
	ErrorText              string
	OutputCompressSavedPct int
	OutputCompressRawRunes int
	OutputCompressOutRunes int
}

func shouldContinueAfterAsyncTool(tail toolResultTailKind) bool {
	return tail == tailTool || tail == tailAssistantWithoutToolCalls
}

func (o *Orchestrator) splitToolResult(sessionID string, tc llm.ToolCall, raw string) (forClient, forHistory, spillPath string) {
	if o.toolHooks == nil {
		return raw, raw, ""
	}
	hc := hooks.BuildToolAfterEachContext(hooks.ToolAfterEachInput{
		SessionID:    sessionID,
		ToolCallID:   tc.ID,
		ToolName:     tc.Function.Name,
		ToolArgs:     parseJSONArgs(tc.Function.Arguments),
		RawArguments: tc.Function.Arguments,
		RawResult:    raw,
	})
	out, err := o.toolHooks.RunPhase(context.Background(), hooks.PhaseToolAfterEach, hc)
	if err != nil {
		return raw, raw, ""
	}
	split := hooks.ToolAfterEachOutputFrom(out)
	return split.ForClient, split.ForHistory, split.SpillPath
}
