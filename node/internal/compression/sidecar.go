package compression

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

const sidecarSyntheticAssistantContent = "上下文过长，请根据上述对话与操作记录准备摘要。"

// summaryUserPrompt 为侧车 StreamChat 最后一条 user 正文（非 system；步骤 4 接入 coordinator）。
const summaryUserPrompt = `根据上方的对话与操作记录，总结可续跑会话所需的上下文。

输出规则：
1. 闲聊、无关信息、纯寒暄不要保留。
2. 按母任务组织；存在多个独立母任务时，逐块输出，块与块之间空一行。
3. 每块固定三行（仅输出摘要正文，不要前言、不要 Markdown、不要调用工具）：
   第一行：[母任务描述]
   第二行：[子任务描述]（无明确子任务时写 [—]）
   第三行：任务目标：…；阶段性总结论：…；修改过的文件和资源：…；做过的计划以及执行情况：…
4. 已完成的任务：第三行尽量简洁，只保留结论与关键产出。如果这个任务跟当前的任务关系已经不大了，并且是已经总结过的任务（即已经以三行式展示的），可以直接删除。
5. 未完成或进行中的任务：第三行须说明进度、已验证的中间结论与阻塞点（如有）。并且越是与当前任务相关联的子任务，越要详细描述。
6. 临时、一次性操作：单独一行 [临时] 简述即可，不必展开为母任务块。
7. 不要编造原文没有的信息；文件与资源使用路径或明确名称。
8. 最后你可以保留一些你认为重要的信息，但是不要超过三行。比如说用户强调的指令、前面总结的经验、整体的计划等。
**9. 不要调用任何的工具，仅输出总结后的内容。**`

// SidecarPrefix 与主 turn 下一步 StreamChat 共享的 system + tools（由 runtime 注入）。
type SidecarPrefix struct {
	SystemPrompt string
	Tools        []tools.ToolDef
}

// SidecarInput 为一次侧车摘要请求；silent 任务启动时应冻结完整快照（含 prefix）。
type SidecarInput struct {
	SidecarPrefix
	Messages      []llm.Message
	End           int
	SidecarAppend sidecarAppendMode
}

// BuildSidecarChatRequest 组装与主 turn 前缀对齐的 StreamChat 请求。
//
// Messages 为 snapshot[leadingSystemSkip:end+1] 加上 ephemeral 尾部（assistant+user 或仅 user）；
// 尾部仅存在于 API 请求，不写回 session。生产路径 leadingSystemSkip 为 0。
func BuildSidecarChatRequest(in SidecarInput, userPrompt string) llm.ChatRequest {
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		userPrompt = summaryUserPrompt
	}

	prefixEnd := in.End
	if prefixEnd < 0 || prefixEnd >= len(in.Messages) {
		prefixEnd = len(in.Messages) - 1
	}
	start := leadingSystemSkip(in.Messages)

	msgs := append([]llm.Message(nil), in.Messages[start:prefixEnd+1]...)
	switch in.SidecarAppend {
	case sidecarAppendAssistantAndUser:
		msgs = append(msgs,
			llm.Message{Role: "assistant", Content: sidecarSyntheticAssistantContent},
			llm.UserMessage(userPrompt, llm.UserNameCompressionSidecar),
		)
	default:
		msgs = append(msgs, llm.UserMessage(userPrompt, llm.UserNameCompressionSidecar))
	}

	toolsCopy := append([]tools.ToolDef(nil), in.Tools...)
	return llm.ChatRequest{
		SystemPrompt: in.SystemPrompt,
		Messages:     msgs,
		Tools:        toolsCopy,
	}
}

// Summarize 通过 StreamChat 生成摘要；不向 Hub 推送流式事件。
//
// 响应含 tool_calls 时忽略 tool_calls，仅取 assistant 正文；正文为空则失败。
func Summarize(ctx context.Context, client llm.Client, req llm.ChatRequest) (content string, usage llm.Usage, err error) {
	if client == nil {
		return "", llm.Usage{}, fmt.Errorf("compression sidecar: llm client is nil")
	}

	var deltas strings.Builder
	var usageOut llm.Usage
	gotUsage := false

	result, err := client.StreamChat(ctx, req, llm.StreamHandler{
		OnDelta: func(delta string) {
			deltas.WriteString(delta)
		},
		OnUsage: func(u llm.Usage) {
			usageOut = u
			gotUsage = true
		},
	})
	if err != nil {
		return "", llm.Usage{}, err
	}

	text := strings.TrimSpace(deltas.String())
	if text == "" {
		text = strings.TrimSpace(result.Content)
	}
	if text == "" {
		return "", usageOut, fmt.Errorf("compression sidecar: empty summary content")
	}
	if !gotUsage {
		return text, llm.Usage{}, nil
	}
	return text, usageOut, nil
}

// hasCompressibleContent 判断压缩区间内是否有可摘要的正文或 tool_calls。
func hasCompressibleContent(messages []llm.Message) bool {
	for _, m := range messages {
		if m.Role == "system" {
			continue
		}
		if strings.TrimSpace(m.Content) != "" || len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}
