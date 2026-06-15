package turn

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

const modelContentMaxChars = 12000

// packagedToolResult 为写入 role=tool 的模型侧正文（简化版 package_tool_result）。
type packagedToolResult struct {
	ModelContent string
	RawRef       string
}

func packageToolResult(toolName, content string) packagedToolResult {
	text := strings.TrimSpace(content)
	if text == "" {
		text = "（空输出）"
	}
	model, _ := clipMiddle(text, modelContentMaxChars)
	rawRef := ""
	if len(text) > modelContentMaxChars {
		rawRef = fmt.Sprintf(".runtime/tool_outputs/%s-truncated", sanitizeToolName(toolName))
	}
	return packagedToolResult{ModelContent: model, RawRef: rawRef}
}

func sanitizeToolName(name string) string {
	out := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == ':' || r == '-' {
			return r
		}
		return '_'
	}, strings.TrimSpace(name))
	out = strings.Trim(out, "_")
	if out == "" {
		return "tool"
	}
	return out
}

func clipMiddle(text string, maxChars int) (string, bool) {
	if len(text) <= maxChars {
		return text, false
	}
	head := maxChars / 2
	tail := maxChars - head
	return text[:head] + fmt.Sprintf("\n[TRUNCATED] 工具输出超过 %d 字符，已保留首尾。\n", maxChars) + text[len(text)-tail:], true
}

type toolResultTailKind string

const (
	tailTool                      toolResultTailKind = "tail_tool"
	tailAssistantWithToolCalls    toolResultTailKind = "tail_assistant_with_tool_calls"
	tailAssistantWithoutToolCalls toolResultTailKind = "tail_assistant_without_tool_calls"
	tailOther                     toolResultTailKind = "other"
)

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

// asyncToolMessages 为 async_tool_result 写回 history 的三段消息（对齐 Python _build_tool_result_messages）。
type asyncToolMessages struct {
	UserMessage            llm.Message
	AssistantMessage       llm.Message
	ToolMessage            llm.Message
	ToolName               string
	ToolCallID             string
	Status                 string
	OutputCompressSavedPct int
	OutputCompressRawRunes int
	OutputCompressOutRunes int
}

func buildAsyncToolMessages(payload AsyncToolResultInput) asyncToolMessages {
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
	toolCallID := strings.TrimSpace(payload.ToolCallID)
	if toolCallID == "" {
		toolCallID = "async-job-" + jobID
	}
	resultBody := strings.TrimSpace(payload.ResultText)
	if status != "succeeded" {
		if errText := strings.TrimSpace(payload.ErrorText); errText != "" {
			resultBody = errText
		} else if resultBody == "" {
			resultBody = "工具执行失败"
		}
	}
	packaged := packageToolResult(toolName, resultBody)
	rawSuffix := ""
	if packaged.RawRef != "" {
		rawSuffix = " raw_ref=" + packaged.RawRef
	}
	toolText := fmt.Sprintf(
		"工具%s执行已完成，job_id：%s，执行结果如下：%s%s",
		toolName, jobID, packaged.ModelContent, rawSuffix,
	)
	userText := fmt.Sprintf("工具%s，job_id已完成，请获取执行结果并继续任务。", toolName)
	argsJSON := fmt.Sprintf(`{"job_id":%q,"tool_name":%q,"status":%q}`, jobID, toolName, status)
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
		ToolName:               toolName,
		ToolCallID:             toolCallID,
		Status:                 status,
		OutputCompressSavedPct: payload.OutputCompressSavedPct,
		OutputCompressRawRunes: payload.OutputCompressRawRunes,
		OutputCompressOutRunes: payload.OutputCompressOutRunes,
	}
}

// AsyncToolResultInput 为 HandleAsyncToolResult 入参。
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
