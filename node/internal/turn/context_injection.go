package turn

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// ContextInjection is model-visible runtime context that is deliberately kept
// out of the durable session history. It is frozen into a
// ModelContextSnapshot and applied to a request copy at the current Turn's
// root user message, rather than appended as a volatile tail.
type ContextInjection struct {
	Name        string                `json:"name"`
	Source      string                `json:"source"`
	Content     string                `json:"content"`
	Position    string                `json:"position"`
	MessageKind llm.MessageSourceKind `json:"message_kind,omitempty"`
	MessageForm llm.MessageSourceForm `json:"message_form,omitempty"`
	LegacyName  string                `json:"legacy_name,omitempty"`
}

const (
	contextInjectionName     = "runtime_context"
	contextInjectionSource   = "runtime_context"
	contextInjectionPosition = "before_current_user"
)

// Message converts the injection to the provider-neutral user-role message
// used by the model request. The message is request-only and must not be
// appended to the session history.
func (c ContextInjection) Message() llm.Message {
	kind := c.MessageKind
	if kind == "" {
		kind = llm.MessageSourceRuntime
		if strings.EqualFold(strings.TrimSpace(c.Source), "memory") || strings.EqualFold(strings.TrimSpace(c.Name), llm.UserNameMemoryContext) {
			kind = llm.MessageSourceMemory
		}
	}
	form := c.MessageForm
	if form == "" {
		form = llm.MessageFormSnapshot
	}
	legacyName := strings.TrimSpace(c.LegacyName)
	if legacyName == "" {
		legacyName = llm.UserNameContext
		if kind == llm.MessageSourceMemory {
			legacyName = llm.UserNameMemoryContext
		}
	}
	source := llm.MessageSource{Kind: kind, Form: form}
	provenance := &llm.MessageProvenance{Producer: c.Source, Operation: c.Name}
	return llm.UserMessageWithSource(c.Content, legacyName, source, provenance)
}

func cloneContextInjections(in []ContextInjection) []ContextInjection {
	if len(in) == 0 {
		return nil
	}
	return append([]ContextInjection(nil), in...)
}

// BuildContextInjections builds the dynamic context portion of a normal
// Agent request. Host/session identity and prompt sidecars are intentionally
// separate from the stable system prompt so changes do not rewrite its
// prefix. The output is deterministic for identical inputs.
func BuildContextInjections(in SystemPromptInput) []ContextInjection {
	var b strings.Builder
	b.WriteString("## 自动注入的运行时上下文\n\n")
	b.WriteString("以下内容由 Node 根据当前运行环境和已配置上下文提供。它不是新的用户消息；应将其作为当前任务的运行事实和背景使用。\n\n")
	if in.TodayDateEnabled && strings.TrimSpace(in.CurrentDate) != "" {
		b.WriteString("## 当前日期\n\n")
		b.WriteString("当天日期为：")
		b.WriteString(strings.TrimSpace(in.CurrentDate))
		b.WriteString("\n\n")
	}
	b.WriteString("## 运行环境\n\n")
	appendEnvironmentSection(&b, environmentSectionInput{
		AgentID:   in.AgentID,
		SessionID: in.SessionID,
		Snapshot:  hostsnapshot.Get(),
	})

	if in.PromptCtx != nil {
		b.WriteString(in.PromptCtx.BuildStableContextSections())
		b.WriteString(in.PromptCtx.BuildCustomSection())
	}

	content := strings.TrimSpace(b.String())
	if content == "" {
		return nil
	}
	return []ContextInjection{{
		Name:        contextInjectionName,
		Source:      contextInjectionSource,
		Content:     content,
		Position:    contextInjectionPosition,
		MessageKind: llm.MessageSourceRuntime,
		MessageForm: llm.MessageFormSnapshot,
		LegacyName:  llm.UserNameContext,
	}}
}

// BuildChildContextInjections is the restricted runtime-context variant for
// child Agents. Child purpose and loaded skills remain in the child system
// prompt; only the environment identity is injected here.
func BuildChildContextInjections(in ChildSystemPromptInput) []ContextInjection {
	var b strings.Builder
	b.WriteString("## 自动注入的运行时上下文\n\n")
	b.WriteString("以下内容由 Node 根据当前运行环境提供，是当前子任务的运行事实，不是新的用户消息。\n\n")
	if in.TodayDateEnabled && strings.TrimSpace(in.CurrentDate) != "" {
		b.WriteString("## 当前日期\n\n")
		b.WriteString("当天日期为：")
		b.WriteString(strings.TrimSpace(in.CurrentDate))
		b.WriteString("\n\n")
	}
	b.WriteString("## 运行环境\n\n")
	appendEnvironmentSection(&b, environmentSectionInput{
		AgentID:   in.AgentID,
		SessionID: in.SessionID,
		Snapshot:  hostsnapshot.Get(),
	})
	return []ContextInjection{{
		Name:        contextInjectionName,
		Source:      contextInjectionSource,
		Content:     strings.TrimSpace(b.String()),
		Position:    contextInjectionPosition,
		MessageKind: llm.MessageSourceRuntime,
		MessageForm: llm.MessageFormSnapshot,
		LegacyName:  llm.UserNameContext,
	}}
}

// ApplyContextInjections creates a request-only history copy. Existing
// context messages are removed defensively so a retry cannot duplicate them.
// The normal anchor is the latest root user message; generated tool/vision/
// compression user messages are not anchors.
func ApplyContextInjections(history []llm.Message, injections []ContextInjection) []llm.Message {
	filtered := make([]llm.Message, 0, len(history)+len(injections))
	for _, message := range history {
		if isRequestOnlyContextMessage(message) {
			continue
		}
		filtered = append(filtered, message)
	}
	if len(injections) == 0 {
		return filtered
	}

	rootIndex := -1
	for i, message := range filtered {
		if isContextRootUser(message) {
			rootIndex = i
		}
	}
	before := make([]ContextInjection, 0, len(injections))
	after := make([]ContextInjection, 0, len(injections))
	for _, injection := range injections {
		if strings.TrimSpace(injection.Content) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(injection.Position), "after_current_user") {
			after = append(after, injection)
		} else {
			// Empty position preserves the original runtime-context behavior.
			before = append(before, injection)
		}
	}

	out := make([]llm.Message, 0, len(filtered)+len(before)+len(after))
	if rootIndex < 0 {
		insertAt := leadingSystemMessages(filtered)
		out = append(out, filtered[:insertAt]...)
		for _, injection := range before {
			out = append(out, injection.Message())
		}
		out = append(out, filtered[insertAt:]...)
		return out
	}
	out = append(out, filtered[:rootIndex]...)
	for _, injection := range before {
		out = append(out, injection.Message())
	}
	out = append(out, filtered[rootIndex])
	for _, injection := range after {
		out = append(out, injection.Message())
	}
	out = append(out, filtered[rootIndex+1:]...)
	return out
}

// StripLegacyTodayDateMessages removes only pre-migration date messages from
// a request copy. The durable history passed by the caller is left unchanged;
// this keeps old transcripts auditable while preventing stale dates from
// consuming model context or entering compression sidecar requests.
func StripLegacyTodayDateMessages(history []llm.Message) []llm.Message {
	if len(history) == 0 {
		return nil
	}
	out := make([]llm.Message, 0, len(history))
	for _, message := range history {
		if llm.IsMessageSource(message, llm.MessageSourceRuntime, llm.MessageFormNotice, llm.UserNameDate) {
			continue
		}
		out = append(out, message)
	}
	return out
}

// StripContextInjections removes request-only context messages from a hook
// result before it is committed back to durable history. Hooks observe the
// complete request copy, but generated context remains owned by the snapshot.
func StripContextInjections(history []llm.Message) []llm.Message {
	if len(history) == 0 {
		return nil
	}
	out := make([]llm.Message, 0, len(history))
	for _, message := range history {
		if isRequestOnlyContextMessage(message) {
			continue
		}
		out = append(out, message)
	}
	return out
}

func isRequestOnlyContextMessage(message llm.Message) bool {
	return llm.IsMessageSource(message, llm.MessageSourceRuntime, llm.MessageFormSnapshot, "") ||
		llm.IsMessageSource(message, llm.MessageSourceMemory, llm.MessageFormSnapshot, "")
}

func leadingSystemMessages(history []llm.Message) int {
	index := 0
	for index < len(history) && history[index].Role == "system" {
		index++
	}
	return index
}

func isContextRootUser(message llm.Message) bool {
	if message.Role != "user" {
		return false
	}
	source := llm.EffectiveMessageSource(message)
	switch source.Kind {
	case llm.MessageSourceUser, llm.MessageSourceTrigger, llm.MessageSourceA2A, llm.MessageSourceChildAgent:
		return true
	default:
		return false
	}
}
