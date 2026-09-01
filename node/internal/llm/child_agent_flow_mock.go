package llm

import (
	"context"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// ChildAgentFlowMock 驱动父 Agent 调用同步 create_temporary_agent 与子 Agent echo 的联调场景。
//
// 父 session（tools 含 create_temporary_agent）：
// 1) 无 tool 结果时返回 create_temporary_agent tool call；
// 2) 已有 tool 结果时返回 FinalReply。
// 子 session：echo 最后一条 user 消息（与 MockClient 一致）。
type ChildAgentFlowMock struct {
	// FinalReply 为父 Agent 收到子 Agent 结果后的最终文本；空则使用默认值。
	FinalReply string
	// CreateArgsJSON 为 create_temporary_agent 的 arguments JSON。
	CreateArgsJSON string
}

const defaultChildAgentCreateArgs = `{"task":"检查 README 是否存在","purpose":"integration test"}`

// StreamChat 按父/子 session 分流模拟 LLM 行为。
func (m *ChildAgentFlowMock) StreamChat(ctx context.Context, req ChatRequest, handler StreamHandler) (ChatResult, error) {
	if m.isParentSession(req.Tools) {
		if m.hasToolResult(req.Messages) {
			text := strings.TrimSpace(m.FinalReply)
			if text == "" {
				text = "子任务已完成"
			}
			m.streamText(ctx, text, handler)
			return ChatResult{Content: text, FinishReason: "stop"}, nil
		}
		args := strings.TrimSpace(m.CreateArgsJSON)
		if args == "" {
			args = defaultChildAgentCreateArgs
		}
		tc := ToolCall{
			ID:   "call-create-child-1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "create_temporary_agent",
				Arguments: args,
			},
		}
		return ChatResult{ToolCalls: []ToolCall{tc}, FinishReason: "tool_calls"}, nil
	}
	echo := &MockClient{}
	return echo.StreamChat(ctx, req, handler)
}

func (m *ChildAgentFlowMock) CompleteText(_ context.Context, _ CompleteRequest) (string, error) {
	return "mock compression summary", nil
}

func (m *ChildAgentFlowMock) NormalizeAssistant(existing []Message, msg Message) Message {
	return (&MockClient{}).NormalizeAssistant(existing, msg)
}

func (m *ChildAgentFlowMock) isParentSession(tools []tools.ToolDef) bool {
	for _, td := range tools {
		if td.Function.Name == "create_temporary_agent" {
			return true
		}
	}
	return false
}

func (m *ChildAgentFlowMock) hasToolResult(messages []Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "tool" {
			return true
		}
		if messages[i].Role == "user" {
			return false
		}
	}
	return false
}

func (m *ChildAgentFlowMock) streamText(ctx context.Context, text string, handler StreamHandler) {
	(&MockClient{}).streamText(ctx, text, handler)
}
