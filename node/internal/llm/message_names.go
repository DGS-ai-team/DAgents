package llm

import "strings"

// User message name constants. Message.Source and Message.Provenance are the
// durable semantic fields; Name is the provider/UI projection.
//
// 见 https://api-docs.deepseek.com/zh-cn/api/create-chat-completion
const (
	UserNameHuman              = "human"
	UserNameTrigger            = "trigger"
	UserNameChildTask          = "child_task"
	UserNameCompression        = "compression"
	UserNameAsyncTool          = "async_tool"
	UserNameCompressionSidecar = "compression_sidecar"
	UserNameToolVision         = "tool_vision"
	UserNameDate               = "date"
	// UserNameSkill marks a durable, model-facing SKILL.md instruction
	// message. It is distinct from a human request and is hidden from the
	// normal transcript projection.
	UserNameSkill = "skill"
	// UserNameContext marks a request-only runtime context injection. These
	// messages are never persisted into the session transcript.
	UserNameContext = "context"
	// UserNameMemoryContext marks a request-only Turn-scoped memory context.
	// It is kept separate from runtime environment context and never persisted.
	UserNameMemoryContext = "memory_context"
)

// UserMessage constructs a plain-text role=user message with structured
// source/provenance.
func UserMessage(content, name string) Message {
	source, provenance := MessageSourceForUserName(name)
	m := UserMessageWithSource(content, name, source, &provenance)
	if strings.TrimSpace(name) == "" {
		// Empty names remain omitted from the provider payload.
		m.Name = ""
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
