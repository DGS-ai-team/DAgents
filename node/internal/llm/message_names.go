package llm

import "strings"

// Legacy user message name constants. New code should use Message.Source and
// Message.Provenance for decisions; Name remains a compatibility projection
// for old history, event payloads, and providers that accept the field.
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

// UserMessage constructs a plain-text role=user message. The legacy name is
// retained, while structured source/provenance are materialized automatically.
func UserMessage(content, name string) Message {
	source, provenance := MessageSourceForUserName(name)
	m := UserMessageWithSource(content, name, source, &provenance)
	if strings.TrimSpace(name) == "" {
		// Preserve the pre-source wire shape: empty legacy names remain omitted.
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
