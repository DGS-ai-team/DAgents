package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// BenchmarkLoadConversationMainWindowHistory keeps the requested window fixed
// while total history grows. It measures the storage read path only; it does
// not include transcript projection, JSON response encoding, IPC, or DOM work.
func BenchmarkLoadConversationMainWindowHistory(b *testing.B) {
	for _, roundCount := range []int{2000, 10000} {
		b.Run(fmt.Sprintf("rounds_%d", roundCount), func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "main-window-benchmark.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			seedMainWindowBenchmarkConversation(b, store, roundCount, 0)

			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := store.LoadConversationMainWindow(ctx, "benchmark-agent", 5, 180); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkLoadConversationMainWindowHighToolVolume measures the same fixed
// five-round main-chat window when the database contains 100 rounds with 1,000
// tool calls each. It verifies that call summaries remain visible while only
// the first page of tool-result bodies is loaded for each uninterrupted group.
func BenchmarkLoadConversationMainWindowHighToolVolume(b *testing.B) {
	const roundCount = 100
	const toolCallsPerRound = 1000
	store, err := Open(filepath.Join(b.TempDir(), "main-window-high-tool-volume.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	seedMainWindowBenchmarkConversation(b, store, roundCount, toolCallsPerRound)

	window, err := store.LoadConversationMainWindow(context.Background(), "benchmark-agent", 5, 180)
	if err != nil {
		b.Fatal(err)
	}
	callSummaries, loadedResults := 0, 0
	for _, record := range window.Records {
		callSummaries += len(record.Payload.ToolCalls)
		if record.Role == "tool" {
			loadedResults++
		}
	}
	if callSummaries != 5*toolCallsPerRound {
		b.Fatalf("loaded %d tool-call summaries, want %d", callSummaries, 5*toolCallsPerRound)
	}
	if loadedResults != 5*initialToolResultCallsPerGroup {
		b.Fatalf("loaded %d tool-result bodies, want %d", loadedResults, 5*initialToolResultCallsPerGroup)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := store.LoadConversationMainWindow(ctx, "benchmark-agent", 5, 180); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(callSummaries), "tool_calls/op")
	b.ReportMetric(float64(loadedResults), "tool_results/op")
}

func seedMainWindowBenchmarkConversation(b testing.TB, store *SQLiteStore, roundCount, toolCallsPerRound int) {
	b.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO conversations(conversation_id,node_id,revision,current_checkpoint_revision,lifecycle_covered_seq,history_completeness,created_at,updated_at) VALUES('benchmark-agent','node',1,1,0,'complete',?,?)`, now, now); err != nil {
		b.Fatal(err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	roundStmt, err := tx.PrepareContext(ctx, `INSERT INTO conversation_rounds(conversation_id,round_id,ordinal,selected_revision_id,pre_checkpoint_revision) VALUES(?,?,?,?,1)`)
	if err != nil {
		b.Fatal(err)
	}
	defer roundStmt.Close()
	revisionStmt, err := tx.PrepareContext(ctx, `INSERT INTO conversation_round_revisions(conversation_id,round_id,revision_id,turn_id,source,generation,status,finish_reason,record_revision,checkpoint_revision,pre_checkpoint_revision) VALUES(?,?,?,?,?,1,'completed','success',1,1,1)`)
	if err != nil {
		b.Fatal(err)
	}
	defer revisionStmt.Close()
	recordStmt, err := tx.PrepareContext(ctx, `INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,?,?, ?,1,'',?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	defer recordStmt.Close()

	for index := 1; index <= roundCount; index++ {
		roundID := fmt.Sprintf("round-%05d", index)
		revisionID := "revision-" + roundID
		if _, err := roundStmt.ExecContext(ctx, "benchmark-agent", roundID, index, revisionID); err != nil {
			b.Fatal(err)
		}
		if _, err := revisionStmt.ExecContext(ctx, "benchmark-agent", roundID, revisionID, "turn-"+roundID, "human"); err != nil {
			b.Fatal(err)
		}
		messages := []llm.Message{{Role: "user", Content: "request", RecordID: roundID + "/user"}}
		if toolCallsPerRound == 0 {
			messages = append(messages,
				llm.Message{Role: "assistant", Content: "response", RecordID: roundID + "/assistant"},
				llm.Message{Role: "assistant", Content: "follow-up", RecordID: roundID + "/follow-up"},
			)
		} else {
			calls := make([]llm.ToolCall, toolCallsPerRound)
			for callIndex := range calls {
				calls[callIndex] = llm.ToolCall{
					ID:   fmt.Sprintf("%s-call-%04d", roundID, callIndex+1),
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "read_file",
						Arguments: `{"path":"benchmark.txt"}`,
					},
				}
			}
			messages = append(messages, llm.Message{Role: "assistant", ToolCalls: calls, RecordID: roundID + "/tool-calls"})
		}
		var recordOrdinal int64 = int64(index) * int64(toolCallsPerRound+3)
		for _, message := range messages {
			payload, err := json.Marshal(message)
			if err != nil {
				b.Fatal(err)
			}
			recordOrdinal++
			if _, err := recordStmt.ExecContext(ctx, "benchmark-agent", message.RecordID, recordOrdinal, roundID, revisionID, message.Role, string(payload), "", now, now); err != nil {
				b.Fatal(err)
			}
		}
		for callIndex := 0; callIndex < toolCallsPerRound; callIndex++ {
			callID := fmt.Sprintf("%s-call-%04d", roundID, callIndex+1)
			result := llm.Message{
				Role:       "tool",
				Name:       "read_file",
				ToolCallID: callID,
				Content:    "ok",
				RecordID:   roundID + "/result-" + fmt.Sprintf("%04d", callIndex+1),
			}
			payload, err := json.Marshal(result)
			if err != nil {
				b.Fatal(err)
			}
			recordOrdinal++
			if _, err := recordStmt.ExecContext(ctx, "benchmark-agent", result.RecordID, recordOrdinal, roundID, revisionID, result.Role, string(payload), callID, now, now); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
}
