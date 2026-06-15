package llm

import "strings"

// user message name 常量：区分上下文中不同来源的 human 消息（对齐 DeepSeek Chat API name 字段）。
//
// 见 https://api-docs.deepseek.com/zh-cn/api/create-chat-completion
const (
	UserNameHuman              = "human"
	UserNameTrigger            = "trigger"
	UserNameA2AInbox           = "a2a_inbox"
	UserNameChildTask          = "child_task"
	UserNameCompression        = "compression"
	UserNameAsyncTool          = "async_tool"
	UserNameCompressionSidecar = "compression_sidecar"
)

// UserMessage 构造 role=user 消息；name 为来源标识，空串则不设置。
func UserMessage(content, name string) Message {
	m := Message{Role: "user", Content: content}
	if n := strings.TrimSpace(name); n != "" {
		m.Name = n
	}
	return m
}

// NormalizeUserMessageName 将空 name 规范为终端用户默认值。
func NormalizeUserMessageName(name string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return UserNameHuman
}
