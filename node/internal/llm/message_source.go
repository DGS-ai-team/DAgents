package llm

import "strings"

// MessageSourceKind identifies the semantic producer class of a message.
// It is deliberately independent from the wire-level role. For example, a
// skill body is a role=user message but has a plugin source.
type MessageSourceKind string

const (
	MessageSourceUser        MessageSourceKind = "user"
	MessageSourceTrigger     MessageSourceKind = "trigger"
	MessageSourceA2A         MessageSourceKind = "a2a"
	MessageSourceChildAgent  MessageSourceKind = "child_agent"
	MessageSourceRuntime     MessageSourceKind = "runtime"
	MessageSourceMemory      MessageSourceKind = "memory"
	MessageSourcePlugin      MessageSourceKind = "plugin"
	MessageSourceAsyncTool   MessageSourceKind = "async_tool"
	MessageSourceCompression MessageSourceKind = "compression"
	MessageSourceTool        MessageSourceKind = "tool"
	MessageSourceModel       MessageSourceKind = "model"
	MessageSourceLegacy      MessageSourceKind = "legacy"
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
// contract and the legacy name field where applicable.
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
// structured source. legacyName remains an internal compatibility field for
// old persistence, existing event consumers, and providers that accept name.
func UserMessageWithSource(content, legacyName string, source MessageSource, provenance *MessageProvenance) Message {
	m := Message{
		Role:    "user",
		Content: content,
		Source:  &source,
	}
	if n := strings.TrimSpace(legacyName); n != "" {
		m.Name = n
	}
	if provenance != nil {
		p := *provenance
		m.Provenance = &p
	}
	return m
}

// MessageSourceForUserName maps the legacy name vocabulary to structured
// source/provenance. This is also the compatibility path for messages loaded
// from pre-source SQLite snapshots.
func MessageSourceForUserName(name string) (MessageSource, MessageProvenance) {
	switch strings.TrimSpace(name) {
	case "", UserNameHuman:
		return MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, MessageProvenance{Producer: "human"}
	case UserNameTrigger:
		return MessageSource{Kind: MessageSourceTrigger, Form: MessageFormRequest}, MessageProvenance{Producer: UserNameTrigger}
	case UserNameA2AInbox:
		return MessageSource{Kind: MessageSourceA2A, Form: MessageFormRelay}, MessageProvenance{Producer: UserNameA2AInbox}
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
		// Unknown names must not silently become human input. Keeping them as a
		// legacy source makes old integrations observable and conservative.
		return MessageSource{Kind: MessageSourceLegacy, Form: MessageFormRequest}, MessageProvenance{Producer: strings.TrimSpace(name)}
	}
}

// EffectiveMessageSource returns the structured source, falling back to the
// legacy role/name representation for old history and hand-written messages.
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
	source, _ := MessageSourceForUserName(message.Name)
	return source
}

// EffectiveMessageProvenance returns explicit provenance or derives the
// compatibility producer from the legacy name/tool identity.
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
	_, provenance := MessageSourceForUserName(message.Name)
	return provenance
}

// NormalizeMessageSource materializes compatibility metadata on a copied
// message. It is useful at boundaries that merge or reinsert an old message
// before persisting it again.
func NormalizeMessageSource(message Message) Message {
	if message.Source == nil {
		source := EffectiveMessageSource(message)
		message.Source = &source
	}
	if message.Provenance == nil {
		provenance := EffectiveMessageProvenance(message)
		message.Provenance = &provenance
	}
	return message
}

// IsMessageSource matches structured source while remaining compatible with
// messages written before source/provenance was introduced.
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
