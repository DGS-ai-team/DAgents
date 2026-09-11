package turn

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func (o *Orchestrator) appendHistory(sessionID string, history *[]llm.Message, message llm.Message) {
	normalized := o.llm.NormalizeAssistant(*history, message)
	if replaceRecoveryPlaceholder(history, normalized) {
		if o.journal != nil {
			// The journal is an append-only audit stream. The snapshot replacement
			// is represented by recording the authoritative result after the
			// placeholder; replay consumers use the latest canonical snapshot.
			o.journal.RecordAppend(sessionID, normalized)
		}
		return
	}
	if o.journal != nil {
		o.journal.AppendMessage(sessionID, history, normalized)
	} else {
		*history = append(*history, normalized)
	}
}

func (o *Orchestrator) insertHistory(sessionID string, history *[]llm.Message, index int, message llm.Message) {
	normalized := o.llm.NormalizeAssistant(*history, message)
	if replaceRecoveryPlaceholder(history, normalized) {
		if o.journal != nil {
			o.journal.RecordAppend(sessionID, normalized)
		}
		return
	}
	if o.journal != nil {
		o.journal.InsertMessage(sessionID, history, index, normalized)
	} else {
		if index < 0 {
			index = 0
		}
		if index > len(*history) {
			index = len(*history)
		}
		out := append([]llm.Message(nil), (*history)[:index]...)
		out = append(out, normalized)
		out = append(out, (*history)[index:]...)
		*history = out
	}
}

func replaceRecoveryPlaceholder(history *[]llm.Message, message llm.Message) bool {
	if history == nil || message.Role != "tool" || strings.TrimSpace(message.ToolCallID) == "" {
		return false
	}
	for index, existing := range *history {
		if existing.Role == "tool" && existing.ToolCallID == message.ToolCallID && llm.IsRecoveryPlaceholderToolResult(existing) {
			(*history)[index] = message
			return true
		}
	}
	return false
}
