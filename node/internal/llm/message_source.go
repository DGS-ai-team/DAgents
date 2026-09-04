package llm

import "strings"

// MessageSourceKind identifies the semantic producer class of a message.
// It is deliberately independent from the wire-level role. For example, a
// skill body is a role=user message but has a plugin source.
type MessageSourceKind string

const (
	MessageSourceUser        MessageSourceKind = "user"
	MessageSourceTrigger     MessageSourceKind = "trigger"
	MessageSourceChildAgent  MessageSourceKind = "child_agent"
	MessageSourceRuntime     MessageSourceKind = "runtime"
	MessageSourceMemory      MessageSourceKind = "memory"
	MessageSourcePlugin      MessageSourceKind = "plugin"
	MessageSourceAsyncTool   MessageSourceKind = "async_tool"
	MessageSourceCompression MessageSourceKind = "compression"
	MessageSourceTool        MessageSourceKind = "tool"
	MessageSourceModel       MessageSourceKind = "model"
)

// MessageSourceForm describes what the producer contributed. Keeping this
// separate from Kind lets one producer publish instructions, snapshots, or
// notices without inventing a new source kind each time.
type MessageSourceForm string

const (
	MessageFormRequest      MessageSourceForm = "request"
	MessageFormRelay        MessageSourceForm = "relay"
	MessageFormInstructions MessageSourceForm = "instructions"
	MessageFormSnapshot     MessageSourceForm = "snapshot"
	MessageFormNotice       MessageSourceForm = "notice"
	MessageFormSummary      MessageSourceForm = "summary"
	MessageFormSidecar      MessageSourceForm = "sidecar"
	MessageFormToolResult   MessageSourceForm = "tool_result"
)

// MessageSource is durable, model-independent provenance about a message.
// It is persisted in internal history but is intentionally excluded from
// provider payloads; providers continue to receive the normal role/content
// contract and the optional name projection where applicable.
type MessageSource struct {
	Kind MessageSourceKind `json:"kind"`
	Form MessageSourceForm `json:"form,omitempty"`
}

// MessageProvenance identifies the concrete producer and operation. Source
// answers "what class of thing is this?"; provenance answers "which runtime
// component produced it and for what operation?".
type MessageProvenance struct {
	Producer  string `json:"producer,omitempty"`
	Operation string `json:"operation,omitempty"`
	Reference string `json:"reference,omitempty"`
}

// UserMessageWithSource constructs a user-role message with explicit
// structured source and the optional provider name projection.
func UserMessageWithSource(content, name string, source MessageSource, provenance *MessageProvenance) Message {
	m := Message{
		Role:    "user",
		Content: content,
		Source:  &source,
	}
	if n := strings.TrimSpace(name); n != "" {
		m.Name = n
	}
	if provenance != nil {
		p := *provenance
		m.Provenance = &p
	}
	return m
}

// MessageSourceForUserName maps the user-message name vocabulary to structured
// source/provenance. Every durable user message should carry this metadata.
func MessageSourceForUserName(name string) (MessageSource, MessageProvenance) {
	switch strings.TrimSpace(name) {
	case "", UserNameHuman:
		return MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, MessageProvenance{Producer: "human"}
	case UserNameTrigger:
		return MessageSource{Kind: MessageSourceTrigger, Form: MessageFormRequest}, MessageProvenance{Producer: UserNameTrigger}
	case UserNameChildTask:
		return MessageSource{Kind: MessageSourceChildAgent, Form: MessageFormRelay}, MessageProvenance{Producer: UserNameChildTask}
	case UserNameContext:
		return MessageSource{Kind: MessageSourceRuntime, Form: MessageFormSnapshot}, MessageProvenance{Producer: "runtime_context"}
	case UserNameMemoryContext:
		return MessageSource{Kind: MessageSourceMemory, Form: MessageFormSnapshot}, MessageProvenance{Producer: "memory"}
	case UserNameSkill:
		return MessageSource{Kind: MessageSourcePlugin, Form: MessageFormInstructions}, MessageProvenance{Producer: "skills"}
	case UserNameDate:
		return MessageSource{Kind: MessageSourceRuntime, Form: MessageFormNotice}, MessageProvenance{Producer: UserNameDate}
	case UserNameAsyncTool:
		return MessageSource{Kind: MessageSourceAsyncTool, Form: MessageFormRelay}, MessageProvenance{Producer: UserNameAsyncTool}
	case UserNameCompression:
		return MessageSource{Kind: MessageSourceCompression, Form: MessageFormSummary}, MessageProvenance{Producer: UserNameCompression}
	case UserNameCompressionSidecar:
		return MessageSource{Kind: MessageSourceCompression, Form: MessageFormSidecar}, MessageProvenance{Producer: UserNameCompressionSidecar}
	case UserNameToolVision:
		return MessageSource{Kind: MessageSourceTool, Form: MessageFormNotice}, MessageProvenance{Producer: UserNameToolVision}
	default:
		return MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, MessageProvenance{Producer: strings.TrimSpace(name)}
	}
}

// EffectiveMessageSource returns the structured source, deriving only the
// role-owned source for assistant/tool messages.
func EffectiveMessageSource(message Message) MessageSource {
	if message.Source != nil && strings.TrimSpace(string(message.Source.Kind)) != "" {
		return *message.Source
	}
	if strings.TrimSpace(message.Role) == "tool" {
		return MessageSource{Kind: MessageSourceTool, Form: MessageFormToolResult}
	}
	if strings.TrimSpace(message.Role) == "assistant" {
		return MessageSource{Kind: MessageSourceModel, Form: MessageFormRequest}
	}
	return MessageSource{}
}

// EffectiveMessageProvenance returns explicit provenance or derives the
// producer for role-owned assistant/tool messages.
func EffectiveMessageProvenance(message Message) MessageProvenance {
	if message.Provenance != nil {
		return *message.Provenance
	}
	if strings.TrimSpace(message.Role) == "tool" {
		return MessageProvenance{Producer: strings.TrimSpace(message.Name), Reference: strings.TrimSpace(message.ToolCallID)}
	}
	if strings.TrimSpace(message.Role) == "assistant" {
		return MessageProvenance{Producer: "model"}
	}
	return MessageProvenance{}
}

// IsMessageSource matches structured message provenance.
func IsMessageSource(message Message, kind MessageSourceKind, form MessageSourceForm, producer string) bool {
	if strings.TrimSpace(message.Role) != "user" && kind != MessageSourceTool {
		return false
	}
	source := EffectiveMessageSource(message)
	if source.Kind != kind {
		return false
	}
	if form != "" && source.Form != form {
		return false
	}
	if strings.TrimSpace(producer) == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(EffectiveMessageProvenance(message).Producer), strings.TrimSpace(producer))
}

// IsHiddenInjectedUserMessage identifies durable user-role context that should
// not be rendered as a normal human bubble during hydrate.
func IsHiddenInjectedUserMessage(message Message) bool {
	if strings.TrimSpace(message.Role) != "user" {
		return false
	}
	source := EffectiveMessageSource(message)
	provenance := EffectiveMessageProvenance(message)
	switch source.Kind {
	case MessageSourcePlugin:
		return source.Form == MessageFormInstructions
	case MessageSourceRuntime:
		return strings.EqualFold(provenance.Producer, UserNameDate)
	case MessageSourceMemory:
		return true
	case MessageSourceAsyncTool, MessageSourceCompression:
		return true
	case MessageSourceTool:
		return strings.EqualFold(provenance.Producer, UserNameToolVision)
	default:
		return false
	}
}
