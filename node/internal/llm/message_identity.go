package llm

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
)

// AssistantDisplayIdentityForStep returns the stable display item identity
// contract for assistant text and reasoning produced by one logical Step.
// Empty Step IDs intentionally produce no identity; callers must not guess one.
func AssistantDisplayIdentityForStep(stepID string) *MessageDisplayIdentity {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return nil
	}
	return &MessageDisplayIdentity{
		ContentItemID:   stepID + "/assistant/content",
		ReasoningItemID: stepID + "/assistant/reasoning",
		ItemVersion:     1,
	}
}

// AssistantMessageIDForStep identifies the logical assistant response for
// lifecycle bookkeeping. It is distinct from the text/reasoning display items.
func AssistantMessageIDForStep(stepID string) string {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return ""
	}
	return stepID + "/assistant"
}

// InputRecordID identifies one accepted queue item. Seq is allocated by the
// InputBox at admission and survives restart, so this is a source identity,
// not a transcript-position or body-derived key.
func InputRecordID(sessionID string, seq uint64) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || seq == 0 {
		return ""
	}
	return sessionID + "/input/" + strconv.FormatUint(seq, 10)
}

// ToolResultRecordID uses the provider call identity already accepted by the
// Coordinator. It intentionally does not depend on tool output content.
func ToolResultRecordID(toolCallID string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}
	return "tool-result/" + toolCallID
}

// StoredRecordID returns an identity that can be proven from existing message
// facts. It intentionally does not derive identity from content or position.
func StoredRecordID(message Message) string {
	if id := strings.TrimSpace(message.RecordID); id != "" {
		return id
	}
	if message.DisplayIdentity != nil {
		if id := strings.TrimSpace(message.DisplayIdentity.ContentItemID); id != "" {
			return id
		}
	}
	if strings.EqualFold(strings.TrimSpace(message.Role), "tool") && strings.TrimSpace(message.ToolCallID) != "" {
		return ToolResultRecordID(message.ToolCallID)
	}
	return ""
}

// NewOpaqueRecordID allocates an identity for migrated legacy items or
// internal records that have no source-level ID. Callers must persist it with
// the record before exposing it; it must never be regenerated on retries.
func NewOpaqueRecordID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "opaque/" + hex.EncodeToString(raw[:]), nil
}
