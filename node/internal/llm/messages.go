package llm

import "strings"

// MessagesWithSystem adds the system prompt before history and promotes
// durable incomplete-turn markers into that prompt without exposing them as
// inline provider messages.
func MessagesWithSystem(systemPrompt string, history []Message) []Message {
	modelHistory := make([]Message, 0, len(history))
	incompleteTargets := make(map[int]string)
	hasUnresolvedIncompleteTurn := false
	hasFileReferences := false
	for index, message := range history {
		if IsTurnIncompleteContextMessage(message) {
			target := findIncompleteUserMessage(history, index, message, incompleteTargets)
			if target >= 0 {
				incompleteTargets[target] = incompleteUserPrefix(message)
			}
			hasUnresolvedIncompleteTurn = true
			continue
		}
		if message.Role == "assistant" && len(message.ToolCalls) == 0 {
			hasUnresolvedIncompleteTurn = false
		}
		if message.Role == "user" && len(message.FileReferences) > 0 {
			hasFileReferences = true
		}
		modelHistory = append(modelHistory, message)
	}
	if len(incompleteTargets) > 0 {
		modelIndex := 0
		for historyIndex, message := range history {
			if IsTurnIncompleteContextMessage(message) {
				continue
			}
			if prefix, incomplete := incompleteTargets[historyIndex]; incomplete {
				modelHistory[modelIndex] = markIncompleteUserMessage(message, prefix)
			}
			modelIndex++
		}
	}
	if hasUnresolvedIncompleteTurn {
		systemPrompt = appendSystemInstruction(systemPrompt, TurnIncompleteSystemInstruction)
	}
	if hasFileReferences {
		systemPrompt = appendSystemInstruction(systemPrompt, FileReferenceSystemInstruction)
	}
	if strings.TrimSpace(systemPrompt) == "" {
		return modelHistory
	}
	out := make([]Message, 0, len(modelHistory)+1)
	out = append(out, Message{Role: "system", Content: systemPrompt})
	out = append(out, modelHistory...)
	return out
}

func appendSystemInstruction(systemPrompt, instruction string) string {
	if strings.TrimSpace(systemPrompt) == "" {
		return instruction
	}
	return strings.TrimSpace(systemPrompt) + "\n\n" + instruction
}

func findIncompleteUserMessage(history []Message, markerIndex int, marker Message, alreadyMarked map[int]string) int {
	turnID := strings.TrimSpace(marker.Provenance.Reference)
	if turnID != "" {
		for index := markerIndex - 1; index >= 0; index-- {
			message := history[index]
			if message.Role == "user" && strings.TrimSpace(EffectiveMessageProvenance(message).Reference) == turnID {
				return index
			}
		}
		// A turn-scoped marker can be persisted before the corresponding user
		// message is appended to history. Never let that race mark an older
		// request; the unscoped fallback below is only for legacy markers.
		return -1
	}
	for index := markerIndex - 1; index >= 0; index-- {
		message := history[index]
		if message.Role != "user" {
			continue
		}
		if _, exists := alreadyMarked[index]; exists {
			continue
		}
		return index
	}
	return -1
}

func incompleteUserPrefix(marker Message) string {
	if strings.EqualFold(strings.TrimSpace(marker.Provenance.Operation), turnFailureOperation) {
		return turnFailureUserPrefix
	}
	return turnCancellationUserPrefix
}
