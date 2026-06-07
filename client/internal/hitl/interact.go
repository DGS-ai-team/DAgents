// Package hitl 处理 Client 侧审批与用户询问交互。
package hitl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

// Sink 为 SSE 事件输出回调；未设置字段时回退到 stdout/stderr。
type Sink struct {
	OnAssistant   func(text string)
	OnReasoning   func(text string)
	OnTool        func(eventType string, data map[string]any)
	OnCompression func(eventType string, data map[string]any)
	OnError       func(msg string)
}

// Interact 为 HITL 交互回调；nil 时回退 stdin 读写。
type Interact struct {
	PromptApproval func(ctx context.Context, data map[string]any) (resume map[string]any, err error)
	PromptUserInfo func(ctx context.Context, data map[string]any) (resume map[string]any, err error)
}

// HandleStreamEvent 处理 SSE 事件；若触发 HITL 则经 Interact 或 stdin 阻塞并 submit resume。

// stopOnDone=true 时 done 返回 continueStream=false（一次性 chat）；false 时保持订阅。
func HandleStreamEvent(
	ctx context.Context,
	client *nodeapi.Client,
	sessionID string,
	ev nodeapi.StreamEvent,
	sink Sink,
	interact *Interact,
	stopOnDone bool,
) (continueStream bool, err error) {
	switch ev.Type {
	case "assistant":
		if text, ok := ev.Data["content"].(string); ok {
			if sink.OnAssistant != nil {
				sink.OnAssistant(text)
			} else {
				fmt.Print(text)
			}
		}
	case "reasoning":
		if text, ok := ev.Data["content"].(string); ok && text != "" {
			if sink.OnReasoning != nil {
				sink.OnReasoning(text)
			}
		}
	case "tool_call", "tool_result":
		if sink.OnTool != nil {
			sink.OnTool(ev.Type, ev.Data)
		} else {
			fmt.Fprintf(os.Stderr, "\n[%s] %v\n", ev.Type, ev.Data)
		}
	case "context_compression_blocking", "context_compression_silent":
		line := FormatContextCompression(ev.Type, ev.Data)
		if sink.OnCompression != nil {
			sink.OnCompression(ev.Type, ev.Data)
		} else {
			fmt.Fprintln(os.Stderr, line)
		}
	case "approval_required":
		rv, err := resolveApproval(ctx, interact, ev.Data)
		if err != nil {
			return true, err
		}
		if err := client.SubmitResume(ctx, sessionID, rv); err != nil {
			return true, err
		}
	case "user_information_required":
		rv, err := resolveUserInformation(ctx, interact, ev.Data)
		if err != nil {
			return true, err
		}
		if err := client.SubmitResume(ctx, sessionID, rv); err != nil {
			return true, err
		}
	case "error":
		msg := strings.TrimSpace(fmt.Sprint(ev.Data["message"]))
		if msg == "" {
			msg = "unknown error"
		}
		if sink.OnError != nil {
			sink.OnError(msg)
		} else {
			fmt.Fprintf(os.Stderr, "\nerror: %s\n", msg)
		}
	case "done":
		if stopOnDone {
			return false, nil
		}
	}
	return true, nil
}

func resolveApproval(ctx context.Context, interact *Interact, data map[string]any) (map[string]any, error) {
	if interact != nil && interact.PromptApproval != nil {
		return interact.PromptApproval(ctx, data)
	}
	prompt := FormatApprovalPrompt(data)
	fmt.Fprintf(os.Stderr, "\n--- 工具审批 ---\n%s\n", prompt)
	approve, err := promptYesNo(os.Stdin, "是否批准执行？(y/N): ")
	if err != nil {
		return nil, err
	}
	return BuildApprovalResume(data, approve), nil
}

func resolveUserInformation(ctx context.Context, interact *Interact, data map[string]any) (map[string]any, error) {
	if interact != nil && interact.PromptUserInfo != nil {
		return interact.PromptUserInfo(ctx, data)
	}
	req := ExtractUserInformationRequest(data)
	if req != nil && len(req.Options) > 0 {
		return resolveUserInformationOptionsPlain(req)
	}
	question := extractUserInformationQuestion(data)
	toolCallID := extractUserInformationToolCallID(data)
	PrintUserInformationTranscript(os.Stderr, &UserInformationRequest{
		ToolCallID: toolCallID,
		Question:   question,
	})
	fmt.Fprint(os.Stderr, "> ")
	answer, err := readLine(os.Stdin)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type":         "user_information",
		"tool_call_id": toolCallID,
		"answer":       answer,
	}, nil
}

// resolveUserInformationOptionsPlain 在终端列出可选项，支持序号或自由文本作答。
func resolveUserInformationOptionsPlain(req *UserInformationRequest) (map[string]any, error) {
	PrintUserInformationTranscript(os.Stderr, req)
	fmt.Fprintln(os.Stderr)
	for i, opt := range req.Options {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, opt.Label)
	}
	prompt := "请输入选项序号"
	if req.AllowMultiple {
		prompt += "（多个用逗号分隔，如 1,3）"
	}
	if !req.Required {
		prompt += "，或直接输入文字"
	}
	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	line, err := readLine(os.Stdin)
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" && req.Required {
		return nil, fmt.Errorf("需要回答")
	}
	if selected, ok := parseOptionSelection(line, req); ok {
		return BuildUserInformationResumeFromOptions(req, selected)
	}
	return BuildUserInformationResume(req, line, nil, false), nil
}

// parseOptionSelection 解析「1」或「1,3」为 option 勾选；非序号格式返回 false。
func parseOptionSelection(line string, req *UserInformationRequest) (map[string]bool, bool) {
	if line == "" || len(req.Options) == 0 {
		return nil, false
	}
	parts := strings.Split(line, ",")
	selected := make(map[string]bool)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := 0
		if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > len(req.Options) {
			return nil, false
		}
		selected[req.Options[idx-1].ID] = true
	}
	if len(selected) == 0 {
		return nil, false
	}
	return selected, true
}

func extractUserInformationQuestion(data map[string]any) string {
	question := strings.TrimSpace(fmt.Sprint(data["content"]))
	if question == "" {
		if args, ok := data["user_information_args"].(map[string]any); ok {
			question = strings.TrimSpace(fmt.Sprint(args["question"]))
		}
	}
	if question == "" {
		question = "请补充信息"
	}
	return question
}

func extractUserInformationToolCallID(data map[string]any) string {
	args, _ := data["user_information_args"].(map[string]any)
	if args != nil {
		if id, ok := args["tool_call_id"].(string); ok {
			return id
		}
	}
	return ""
}

// FormatUserInformationQuestion 提取询问正文（供全屏 TUI 展示）。
func FormatUserInformationQuestion(data map[string]any) string {
	return extractUserInformationQuestion(data)
}

// ExtractUserInformationToolCallID 从 SSE data 提取 tool_call_id。
func ExtractUserInformationToolCallID(data map[string]any) string {
	return extractUserInformationToolCallID(data)
}

func promptYesNo(r *os.File, prompt string) (bool, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := readLine(r)
	if err != nil {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func readLine(r *os.File) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("empty input")
	}
	return scanner.Text(), nil
}
