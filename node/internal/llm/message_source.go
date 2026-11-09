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

const (
	turnCancellationProducer  = "turn_runtime"
	turnCancellationOperation = "cancelled_turn_context"
	turnFailureOperation      = "failed_turn_context"
)

// TurnIncompleteSystemInstruction is injected while an incomplete request is
// still unresolved by a later completed assistant response. It covers both
// cancellation and terminal model failure without removing canonical history.
const TurnIncompleteSystemInstruction = "运行时状态：对话中有用户请求在完成前结束，相关请求已在原消息旁明确标记。优先响应最新用户消息；只有当最新用户消息明确要求恢复某一条未完成请求时，才继续该请求。"

const turnCancellationUserPrefix = "[运行时标记：此用户请求在完成前已取消。除非最新用户消息明确要求恢复，否则不要继续执行此请求。]\n\n"
const turnFailureUserPrefix = "[运行时标记：此用户请求未成功完成。不要因后续新消息而隐式重放；只有用户明确要求恢复时才继续。]\n\n"

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

// TurnCancellationContextMessage is a durable, transcript-hidden system note
// that prevents an unanswered cancelled request from being mistaken for the
// latest user intent in a later model call.
func TurnCancellationContextMessage(turnID string) Message {
	return turnIncompleteContextMessage(turnID, turnCancellationOperation)
}

// TurnFailureContextMessage marks a request that ended with a terminal model
// failure. The marker remains in canonical history; provider projection uses
// it to avoid implicitly retrying that old request after a later user message.
func TurnFailureContextMessage(turnID string) Message {
	return turnIncompleteContextMessage(turnID, turnFailureOperation)
}

func turnIncompleteContextMessage(turnID, operation string) Message {
	return Message{
		Role:    "system",
		Content: TurnIncompleteSystemInstruction,
		Source:  &MessageSource{Kind: MessageSourceRuntime, Form: MessageFormNotice},
		Provenance: &MessageProvenance{
			Producer:  turnCancellationProducer,
			Operation: operation,
			Reference: strings.TrimSpace(turnID),
		},
	}
}

// IsTurnIncompleteContextMessage identifies internal durable markers for turns
// that ended before completion.
func IsTurnIncompleteContextMessage(message Message) bool {
	return strings.TrimSpace(message.Role) == "system" &&
		message.Source != nil && message.Source.Kind == MessageSourceRuntime && message.Source.Form == MessageFormNotice &&
		message.Provenance != nil &&
		strings.EqualFold(strings.TrimSpace(message.Provenance.Producer), turnCancellationProducer) &&
		(strings.EqualFold(strings.TrimSpace(message.Provenance.Operation), turnCancellationOperation) ||
			strings.EqualFold(strings.TrimSpace(message.Provenance.Operation), turnFailureOperation))
}

// IsTurnCancellationContextMessage identifies the cancellation-specific form.
func IsTurnCancellationContextMessage(message Message) bool {
	return IsTurnIncompleteContextMessage(message) && strings.EqualFold(strings.TrimSpace(message.Provenance.Operation), turnCancellationOperation)
}

func markIncompleteUserMessage(message Message, prefix string) Message {
	if strings.TrimSpace(message.Role) != "user" {
		return message
	}
	if len(message.ContentParts) == 0 {
		if !strings.HasPrefix(message.Content, prefix) {
			message.Content = prefix + message.Content
		}
		return message
	}
	message.ContentParts = append([]ContentPart(nil), message.ContentParts...)
	for index, part := range message.ContentParts {
		if part.Type != "text" {
			continue
		}
		if !strings.HasPrefix(part.Text, prefix) {
			message.ContentParts[index].Text = prefix + part.Text
		}
		return message
	}
	message.ContentParts = append([]ContentPart{{Type: "text", Text: prefix}}, message.ContentParts...)
	return message
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
	case UserNameConversationBoundary:
		return MessageSource{Kind: MessageSourceRuntime, Form: MessageFormNotice}, MessageProvenance{Producer: "idle_context", Operation: "continue-from-archive"}
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

// IsPartialAssistantMessage identifies a text-only assistant fragment that was
// persisted after a provider failure. It remains ordinary model history while
// allowing hydrate clients to distinguish it from a completed response.
func IsPartialAssistantMessage(message Message) bool {
	return strings.EqualFold(strings.TrimSpace(message.Role), "assistant") &&
		message.Provenance != nil && strings.EqualFold(strings.TrimSpace(message.Provenance.Operation), "partial_response")
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
		return strings.EqualFold(provenance.Producer, UserNameDate) || source.Form == MessageFormNotice && strings.EqualFold(provenance.Producer, "idle_context")
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
