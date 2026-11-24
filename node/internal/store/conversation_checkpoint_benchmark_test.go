package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// BenchmarkPersistRuntimeCheckpoint measures the full-working-set checkpoint
// serialization and SQLite commit cost as a baseline for context resets.
func BenchmarkPersistRuntimeCheckpoint(b *testing.B) {
	for _, count := range []int{100, 500, 2_000, 10_000} {
		b.Run(fmt.Sprintf("messages_%d", count), func(b *testing.B) {
			st, err := Open(filepath.Join(b.TempDir(), "checkpoint.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer st.Close()

			messages := make([]llm.Message, count)
			for i := range messages {
				messages[i] = llm.Message{
					Role:    "assistant",
					Content: strings.Repeat("x", 256),
				}
			}
			rec := Record{AgentID: "checkpoint-benchmark", NodeID: "benchmark"}
			ctx := context.Background()
			var revision uint64
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := st.PersistRuntimeDelta(ctx, rec, &ConversationDelta{
					ConversationID:   rec.AgentID,
					NodeID:           rec.NodeID,
					ExpectedRevision: revision,
					Checkpoint:       &CheckpointCommit{Messages: messages, Lineage: "benchmark"},
				})
				if err != nil {
					b.Fatal(err)
				}
				revision = result.ConversationRevision
			}
		})
	}
}

func BenchmarkPersistRuntimeCheckpointDelta(b *testing.B) {
	for _, count := range []int{100, 500, 2_000, 10_000} {
		b.Run(fmt.Sprintf("messages_%d", count), func(b *testing.B) {
			st, err := Open(filepath.Join(b.TempDir(), "checkpoint-delta.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer st.Close()

			messages := make([]llm.Message, count)
			for i := range messages {
				messages[i] = llm.Message{Role: "assistant", Content: strings.Repeat("x", 256)}
			}
			rec := Record{AgentID: "checkpoint-delta-benchmark", NodeID: "benchmark"}
			ctx := context.Background()
			base, err := st.PersistRuntimeDelta(ctx, rec, &ConversationDelta{
				ConversationID: rec.AgentID, NodeID: rec.NodeID,
				Checkpoint: &CheckpointCommit{Messages: messages, Lineage: "benchmark_base"},
			})
			if err != nil {
				b.Fatal(err)
			}
			conversationRevision := base.ConversationRevision
			checkpointRevision := base.CheckpointRevision
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				message := llm.Message{Role: "assistant", Content: "incremental result"}
				result, err := st.PersistRuntimeDelta(ctx, rec, &ConversationDelta{
					ConversationID:   rec.AgentID,
					NodeID:           rec.NodeID,
					ExpectedRevision: conversationRevision,
					Checkpoint: &CheckpointCommit{Lineage: "live_append", Delta: &CheckpointDelta{
						BaseRevision: checkpointRevision,
						Operations:   []CheckpointOperation{{Kind: "append", Message: message}},
					}},
				})
				if err != nil {
					b.Fatal(err)
				}
				conversationRevision = result.ConversationRevision
				checkpointRevision = result.CheckpointRevision
			}
		})
	}
}
