package turn

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func (o *Orchestrator) appendHistory(sessionID string, history *[]llm.Message, message llm.Message) {
	normalized := o.llm.NormalizeAssistant(*history, message)
	if index := recoveryPlaceholderIndex(history, normalized); index >= 0 {
		normalized = o.normalizeHistoryMutation(HistoryMutationReplace, index, normalized)
		(*history)[index] = normalized
		if o.journal != nil {
			// The journal is an append-only audit stream. The snapshot replacement
			// is represented by recording the authoritative result after the
			// placeholder; replay consumers use the latest canonical snapshot.
			o.journal.RecordAppend(sessionID, normalized)
		}
		return
	}
	if index := historyRecordIDIndex(history, normalized); index >= 0 {
		normalized = o.normalizeHistoryMutation(HistoryMutationReplace, index, normalized)
		(*history)[index] = normalized
		if o.journal != nil {
			o.journal.RecordAppend(sessionID, normalized)
		}
		return
	}
	normalized = o.normalizeHistoryMutation(HistoryMutationAppend, len(*history), normalized)
	if o.journal != nil {
		o.journal.AppendMessage(sessionID, history, normalized)
	} else {
		*history = append(*history, normalized)
	}
}

func (o *Orchestrator) insertHistory(sessionID string, history *[]llm.Message, index int, message llm.Message) {
	normalized := o.llm.NormalizeAssistant(*history, message)
	if placeholderIndex := recoveryPlaceholderIndex(history, normalized); placeholderIndex >= 0 {
		normalized = o.normalizeHistoryMutation(HistoryMutationReplace, placeholderIndex, normalized)
		(*history)[placeholderIndex] = normalized
		if o.journal != nil {
			o.journal.RecordAppend(sessionID, normalized)
		}
		return
	}
	if duplicateIndex := historyRecordIDIndex(history, normalized); duplicateIndex >= 0 {
		normalized = o.normalizeHistoryMutation(HistoryMutationReplace, duplicateIndex, normalized)
		(*history)[duplicateIndex] = normalized
		if o.journal != nil {
			o.journal.RecordAppend(sessionID, normalized)
		}
		return
	}
	if index < 0 {
		index = 0
	}
	if index > len(*history) {
		index = len(*history)
	}
	normalized = o.normalizeHistoryMutation(HistoryMutationInsert, index, normalized)
	if o.journal != nil {
		o.journal.InsertMessage(sessionID, history, index, normalized)
	} else {
		out := append([]llm.Message(nil), (*history)[:index]...)
		out = append(out, normalized)
		out = append(out, (*history)[index:]...)
		*history = out
	}
}

func recoveryPlaceholderIndex(history *[]llm.Message, message llm.Message) int {
	if history == nil || message.Role != "tool" || strings.TrimSpace(message.ToolCallID) == "" {
		return -1
	}
	for index, existing := range *history {
		if existing.Role == "tool" && existing.ToolCallID == message.ToolCallID && llm.IsRecoveryPlaceholderToolResult(existing) {
			return index
		}
	}
	return -1
}

func historyRecordIDIndex(history *[]llm.Message, message llm.Message) int {
	if history == nil {
		return -1
	}
	id := llm.StoredRecordID(message)
	if id == "" {
		return -1
	}
	switch strings.ToLower(strings.TrimSpace(message.Role)) {
	case "assistant", "tool":
		// Partial assistant revisions and an already-recorded tool result are
		// adjacent to the end of the active working set. Recovery placeholders
		// are located by their dedicated tool-call identity check above.
		if len(*history) > 0 && llm.StoredRecordID((*history)[len(*history)-1]) == id {
			return len(*history) - 1
		}
	case "user":
		// An accepted input may already be present at the beginning of a resumed
		// model working set. This is one check per root input, not per streamed
		// token or tool result.
		for index, existing := range *history {
			if llm.StoredRecordID(existing) == id {
				return index
			}
		}
	}
	return -1
}
