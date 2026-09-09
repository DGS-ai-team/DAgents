package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// DurableMaintenanceSource is implemented by the session SQLite adapter.
// Keeping this interface here avoids coupling the memory domain to the
// runtime persistence package (which itself depends on turn/llm types).
type DurableMaintenanceSource interface {
	LoadMaintenanceMessages(context.Context, string, uint64, int) (DurableMessageBatch, error)
}

type DurableMessageBatch struct {
	SessionID string
	Sequence  uint64
	Messages  []llm.Message
	Complete  bool
}

// ReadDurableMaintenanceInput reads the SQLite transcript snapshot used by
// runtime recovery. The optional JSONL journal is intentionally not involved:
// it is an audit sidecar and may be disabled or incomplete.
func ReadDurableMaintenanceInput(ctx context.Context, source DurableMaintenanceSource, agentID string, cursor MaintenanceCursor) (ExtractionInput, int64, bool, error) {
	if source == nil || strings.TrimSpace(agentID) == "" {
		return ExtractionInput{}, cursor.Sequence, false, fmt.Errorf("maintenance source and agent id are required")
	}
	if cursor.Sequence < 0 {
		return ExtractionInput{}, cursor.Sequence, false, fmt.Errorf("maintenance cursor sequence is negative")
	}
	batch, err := source.LoadMaintenanceMessages(ctx, agentID, uint64(cursor.Sequence), 200)
	if err != nil {
		return ExtractionInput{}, cursor.Sequence, false, err
	}
	messages, revision := batch.Messages, batch.Sequence
	if revision > math.MaxInt64 {
		return ExtractionInput{}, cursor.Sequence, false, fmt.Errorf("maintenance input sequence overflows int64: %d", revision)
	}
	if !batch.Complete {
		if revision == 0 {
			return ExtractionInput{}, cursor.Sequence, false, nil
		}
		if revision < uint64(cursor.Sequence) {
			return ExtractionInput{}, cursor.Sequence, false, fmt.Errorf("maintenance input sequence regressed: got=%d want at least %d", revision, cursor.Sequence)
		}
		if revision > uint64(cursor.Sequence) {
			return ExtractionInput{}, cursor.Sequence, false, fmt.Errorf("maintenance input at sequence %d is unreadable", revision)
		}
		return ExtractionInput{}, cursor.Sequence, false, nil
	}
	if revision < uint64(cursor.Sequence) {
		return ExtractionInput{}, cursor.Sequence, false, fmt.Errorf("maintenance input sequence regressed: got=%d want at least %d", revision, cursor.Sequence)
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		return ExtractionInput{}, cursor.Sequence, false, err
	}
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:])
	sequence := int64(revision)
	if sequence == cursor.Sequence && fingerprint == cursor.SourceFingerprint {
		return ExtractionInput{}, sequence, false, nil
	}
	inputMessages := make([]ExtractionMessage, 0, len(messages))
	for _, message := range messages {
		inputMessages = append(inputMessages, extractionMessage(message))
	}
	return ExtractionInput{AgentID: strings.TrimSpace(agentID), SessionID: batch.SessionID, Scope: ScopeAgent, SourceFingerprint: fingerprint, Messages: inputMessages}, sequence, true, nil
}

func extractionMessage(message llm.Message) ExtractionMessage {
	out := ExtractionMessage{Role: message.Role, Name: message.Name, Content: llm.MessageTextSummary(message), ToolCallID: message.ToolCallID}
	for _, call := range message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ExtractionToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	return out
}
