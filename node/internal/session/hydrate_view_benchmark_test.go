package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// BenchmarkHydrateRuntimeWorkingSet keeps durable history fixed and grows the
// active model working set. Hydrate should read the bounded durable transcript
// plus pending record deltas, not clone or scan unrelated model context.
func BenchmarkHydrateRuntimeWorkingSet(b *testing.B) {
	for _, messageCount := range []int{2000, 10000} {
		b.Run(fmt.Sprintf("runtime_messages_%d", messageCount), func(b *testing.B) {
			st, err := store.Open(filepath.Join(b.TempDir(), "hydrate-benchmark.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer st.Close()
			if err := st.SaveConversationSnapshot(context.Background(), store.Record{
				AgentID: "hydrate-benchmark-agent",
				NodeID:  "node",
				Messages: []llm.Message{
					{Role: "user", Content: "recent request", RecordID: "recent/user"},
					{Role: "assistant", Content: "recent response", RecordID: "recent/assistant"},
				},
			}); err != nil {
				b.Fatal(err)
			}

			manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
			defer manager.Stop()
			rt := newLifecycleTestRuntime()
			rt.session.ID = "hydrate-benchmark-agent"
			rt.store = st
			rt.queue = queue.NewMessageQueue()
			rt.messages = make([]llm.Message, messageCount)
			for index := range rt.messages {
				rt.messages[index] = llm.Message{Role: "user", Content: "unprojected model context"}
			}
			manager.sessions[rt.session.ID] = rt

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := manager.GetHydrateView(rt.session.ID); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkColdHydrateQueueProjection compares the old cold-hydrate path that
// loaded and decoded every queue payload with the kind-filtered user projection.
// The mailbox is bounded to 256 rows, but internal trigger payloads can be much
// larger than the small user-visible slice.
func BenchmarkColdHydrateQueueProjection(b *testing.B) {
	st, err := store.Open(filepath.Join(b.TempDir(), "hydrate-queue-projection.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()

	const queueSize = 256
	content := strings.Repeat("x", 4*1024)
	rows := make([]store.InputQueueRow, 0, queueSize)
	for seq := uint64(1); seq <= queueSize; seq++ {
		kind := InputKindTrigger
		if (seq-1)%16 == 0 {
			kind = InputKindUser
		}
		record := InputRecord{Seq: seq, Revision: 1, Kind: kind, Env: queue.Envelope{RequestType: queue.RequestTypeMessage, Content: content}}
		payload, err := json.Marshal(record)
		if err != nil {
			b.Fatal(err)
		}
		rows = append(rows, store.InputQueueRow{ConversationEpoch: 1, InputSeq: seq, InputRevision: 1, Kind: string(kind), Status: "queued", PayloadJSON: payload})
	}
	state := store.RuntimeState{InputBoxState: json.RawMessage(`{"seq":256,"conversation_epoch":1,"queue_revision":256,"queue_storage_version":1}`)}
	if _, err := st.PersistRuntimeDelta(context.Background(), store.Record{AgentID: "hydrate-queue-benchmark", NodeID: "node", RuntimeState: state, InputQueueRows: rows}, nil); err != nil {
		b.Fatal(err)
	}

	for _, benchmark := range []struct {
		name      string
		load      func() ([]store.InputQueueRow, error)
		bytesRead int64
	}{
		{
			name: "all-kinds",
			load: func() ([]store.InputQueueRow, error) {
				return st.LoadInputQueue(context.Background(), "hydrate-queue-benchmark")
			},
			bytesRead: queueSize * int64(len(content)),
		},
		{
			name: "user-kind-only",
			load: func() ([]store.InputQueueRow, error) {
				return st.LoadInputQueueByKind(context.Background(), "hydrate-queue-benchmark", string(InputKindUser))
			},
			bytesRead: ((queueSize + 15) / 16) * int64(len(content)),
		},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(benchmark.bytesRead)
			b.ResetTimer()
			for range b.N {
				loaded, err := benchmark.load()
				if err != nil {
					b.Fatal(err)
				}
				if _, err := pendingUserInputsFromQueueRows(loaded); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
