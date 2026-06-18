package hitl

import (
	"fmt"
	"io"
	"strings"
)

// UserInformationOption 为 ask_user_information 的单个可选项。
type UserInformationOption struct {
	ID    string
	Label string
	Value string
}

// UserInformationRequest 为 SSE user_information_required 解析结果。
type UserInformationRequest struct {
	ToolCallID    string
	Question      string
	Options       []UserInformationOption
	AllowMultiple bool
	Placeholder   string
	Required      bool
}

// ExtractUserInformationRequest 从 SSE data 提取询问请求；无效时返回 nil。
func ExtractUserInformationRequest(data map[string]any) *UserInformationRequest {
	if data == nil {
		return nil
	}
	args, _ := data["user_information_args"].(map[string]any)
	if args == nil {
		return nil
	}
	toolCallID := strings.TrimSpace(fmt.Sprint(args["tool_call_id"]))
	question := strings.TrimSpace(fmt.Sprint(args["question"]))
	if question == "" {
		question = strings.TrimSpace(fmt.Sprint(data["content"]))
	}
	if toolCallID == "" || question == "" {
		return nil
	}
	req := &UserInformationRequest{
		ToolCallID:    toolCallID,
		Question:      question,
		AllowMultiple: boolArg(args["allow_multiple"]),
		Placeholder:   strings.TrimSpace(fmt.Sprint(args["placeholder"])),
		Required:      boolArgDefault(args["required"], true),
	}
	rawOptions, _ := args["options"].([]any)
	for _, raw := range rawOptions {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(m["id"]))
		label := strings.TrimSpace(fmt.Sprint(m["label"]))
		if id == "" || label == "" {
			continue
		}
		value := strings.TrimSpace(fmt.Sprint(m["value"]))
		if value == "" {
			value = label
		}
		req.Options = append(req.Options, UserInformationOption{ID: id, Label: label, Value: value})
	}
	return req
}

// BuildUserInformationResume 构造 user_information resume_value。
func BuildUserInformationResume(req *UserInformationRequest, answer string, selectedIDs []string, cancelled bool) map[string]any {
	if req == nil {
		return map[string]any{"type": "user_information"}
	}
	rv := map[string]any{
		"type":         "user_information",
		"tool_call_id": req.ToolCallID,
		"answer":       strings.TrimSpace(answer),
		"selected_options": append([]string(nil), selectedIDs...),
	}
	if cancelled {
		rv["cancelled"] = true
	}
	return rv
}

// BuildUserInformationResumeFromOptions 根据选中 option id 构造 resume（answer 为 label 拼接）。
func BuildUserInformationResumeFromOptions(req *UserInformationRequest, selected map[string]bool) (map[string]any, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}
	selectedIDs := make([]string, 0)
	labels := make([]string, 0)
	for _, opt := range req.Options {
		if selected[opt.ID] {
			selectedIDs = append(selectedIDs, opt.ID)
			labels = append(labels, opt.Label)
		}
	}
	if len(selectedIDs) == 0 && req.Required {
		return nil, fmt.Errorf("请至少选择一项")
	}
	return BuildUserInformationResume(req, strings.Join(labels, ", "), selectedIDs, false), nil
}

// PrintUserInformationTranscript 向 w 输出合并后的「Agent 询问」块（REPL stdin 路径）。
func PrintUserInformationTranscript(w io.Writer, req *UserInformationRequest) {
	for _, line := range FormatUserInformationTranscriptLines(req) {
		fmt.Fprintln(w, line)
	}
}

// FormatUserInformationTranscriptLines 将询问合并为 transcript 单条（对齐 Python TUI「Agent 询问」块）。
func FormatUserInformationTranscriptLines(req *UserInformationRequest) []string {
	if req == nil || strings.TrimSpace(req.Question) == "" {
		return nil
	}
	lines := []string{"[tool] ● Agent 询问"}
	for _, part := range strings.Split(req.Question, "\n") {
		lines = append(lines, "    "+strings.TrimRight(part, " \t"))
	}
	return lines
}

// FormatUserInformationOptions 格式化选项列表（供 TUI 展示）。
func FormatUserInformationOptions(req *UserInformationRequest, selected map[string]bool, cursor int) string {
	if req == nil || len(req.Options) == 0 {
		return ""
	}
	var b strings.Builder
	for i, opt := range req.Options {
		mark := "[ ]"
		if selected[opt.ID] {
			mark = "[x]"
		}
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%s %s\n", prefix, mark, opt.Label)
	}
	hint := "↑/↓ 移动 · Space 切换 · Enter 确认"
	if req.AllowMultiple {
		hint += "（可多选）"
	} else {
		hint += "（单选）"
	}
	fmt.Fprintf(&b, "\n%s", hint)
	return strings.TrimRight(b.String(), "\n")
}

func boolArg(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	default:
		return false
	}
}

func boolArgDefault(v any, def bool) bool {
	if v == nil {
		return def
	}
	return boolArg(v)
}
