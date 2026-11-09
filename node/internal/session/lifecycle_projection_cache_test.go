package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func appendProjectionEvent(t testing.TB, st *store.SQLiteStore, eventType turn.EventType, commandID string, payload string) turn.TurnEventEnvelope {
	t.Helper()
	event, err := st.AppendTurnEvent(context.Background(), turn.TurnEventEnvelope{
		SessionID: "projection-session",
		AgentID:   "agent-1",
		TurnID:    "turn-1",
		StepID:    "step-1",
		EventType: eventType,
		CommandID: commandID,
		Payload:   json.RawMessage(payload),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("append lifecycle event %s: %v", commandID, err)
	}
	return event
}

func seedProjectionEvents(t testing.TB, st *store.SQLiteStore, facts int) {
	t.Helper()
	appendProjectionEvent(t, st, turn.EventTurnStarted, "turn-start", `{"generation":1}`)
	appendProjectionEvent(t, st, turn.EventStepStarted, "step-start", `{"generation":1}`)
	for index := 0; index < facts; index++ {
		payload := fmt.Sprintf(`{"generation":1,"external_fact_id":"fact-%d","external_fact_kind":"test"}`, index)
		appendProjectionEvent(t, st, turn.EventExternalFactRecorded, fmt.Sprintf("fact-command-%d", index), payload)
	}
}

func TestLifecycleProjectionCacheInvalidatesOnAppendAndReturnsClones(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "projection-cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedProjectionEvents(t, st, 1)
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, nil)

	first, hasEvents, firstSequence, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1")
	if err != nil || !hasEvents || first.ExternalFacts != 1 || firstSequence != 3 {
		t.Fatalf("first projection = %#v, events=%v seq=%d err=%v", first, hasEvents, firstSequence, err)
	}
	first.ExternalFacts = 99
	appendProjectionEvent(t, st, turn.EventExternalFactRecorded, "fact-command-1", `{"generation":1,"external_fact_id":"fact-1","external_fact_kind":"test"}`)

	second, hasEvents, secondSequence, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1")
	if err != nil || !hasEvents || second.ExternalFacts != 2 || secondSequence != 4 {
		t.Fatalf("updated projection = %#v, events=%v seq=%d err=%v", second, hasEvents, secondSequence, err)
	}
	second.ExternalFacts = 77
	third, _, _, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1")
	if err != nil || third.ExternalFacts != 2 {
		t.Fatalf("cached projection leaked caller mutation: %#v err=%v", third, err)
	}
}

func TestLifecycleProjectionCacheConcurrentReadersSeeAppendedEvents(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "projection-concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedProjectionEvents(t, st, 0)
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, nil)
	var wg sync.WaitGroup
	readErrors := make(chan error, 8)
	for reader := 0; reader < 8; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < 25; iteration++ {
				if _, _, _, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1"); err != nil {
					readErrors <- err
					return
				}
			}
		}()
	}
	for index := 0; index < 20; index++ {
		appendProjectionEvent(t, st, turn.EventExternalFactRecorded, fmt.Sprintf("concurrent-fact-%d", index), fmt.Sprintf(`{"generation":1,"external_fact_id":"concurrent-%d","external_fact_kind":"test"}`, index))
	}
	wg.Wait()
	close(readErrors)
	for err := range readErrors {
		t.Fatal(err)
	}
	final, _, sequence, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1")
	if err != nil || final.ExternalFacts != 20 || sequence != 22 {
		t.Fatalf("final projection = %#v seq=%d err=%v", final, sequence, err)
	}
}

func TestEmptyLifecycleProjectionCacheInvalidatesOnFirstAppend(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "projection-empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, nil)
	if snapshot, hasEvents, sequence, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1"); err != nil || hasEvents || sequence != 0 || snapshot.HasActiveTurn {
		t.Fatalf("empty projection = %#v events=%v seq=%d err=%v", snapshot, hasEvents, sequence, err)
	}
	seedProjectionEvents(t, st, 0)
	snapshot, hasEvents, sequence, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1")
	if err != nil || !hasEvents || sequence != 2 || !snapshot.HasActiveTurn {
		t.Fatalf("projection after first append = %#v events=%v seq=%d err=%v", snapshot, hasEvents, sequence, err)
	}
}

func TestLifecycleProjectionFailureDoesNotPoisonCache(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "projection-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedProjectionEvents(t, st, 1)
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := manager.loadLifecycleProjection(ctx, "projection-session", "agent-1"); err == nil {
		t.Fatal("cancelled version read should fail")
	}
	snapshot, hasEvents, sequence, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1")
	if err != nil || !hasEvents || snapshot.ExternalFacts != 1 || sequence != 3 {
		t.Fatalf("projection after failed read = %#v events=%v seq=%d err=%v", snapshot, hasEvents, sequence, err)
	}
}

func TestCloneCoordinatorSnapshotDetachesMutableFields(t *testing.T) {
	finished := time.Now().UTC()
	source := turn.CoordinatorSnapshot{
		TurnFinishedAt:     &finished,
		InteractionPayload: json.RawMessage(`{"state":"waiting"}`),
		ActiveToolCalls:    []turn.ToolCallProjection{{ID: "call-1", Arguments: json.RawMessage(`{"x":1}`)}},
		ToolExecutions:     []turn.ToolExecutionView{{ID: "execution-1"}},
	}
	clone := cloneCoordinatorSnapshot(source)
	clone.InteractionPayload[0] = ' '
	clone.ActiveToolCalls[0].Arguments[0] = ' '
	clone.ToolExecutions[0].ID = "changed"
	clone.TurnFinishedAt = nil
	if string(source.InteractionPayload) != `{"state":"waiting"}` || string(source.ActiveToolCalls[0].Arguments) != `{"x":1}` || source.ToolExecutions[0].ID != "execution-1" || source.TurnFinishedAt == nil {
		t.Fatalf("snapshot clone mutated source: %#v", source)
	}
}

func TestCloneContextMessagesDetachesNestedHistory(t *testing.T) {
	messages := []llm.Message{{
		Role:           "user",
		ContentParts:   []llm.ContentPart{{Type: "image_url", ImageURL: &llm.ImageURLPart{URL: "data:image/png;base64,abc"}}},
		FileReferences: []llm.FileReference{{Path: "C:/tmp/source.png", Name: "source.png", Anchor: &llm.FileReferenceAnchor{Type: "pdf-region", Label: "第 2 页", Location: map[string]any{"page": 2}}}},
		ToolCalls:      []llm.ToolCall{{ID: "call-1", Function: llm.ToolCallFunction{Name: "bash_run"}}},
		Source:         &llm.MessageSource{Kind: llm.MessageSourceUser},
		Provenance:     &llm.MessageProvenance{Producer: "human"},
		ToolResultMetadata: &llm.ToolResultMetadata{
			Status: "error", Error: &llm.ToolResultErrorDetail{Code: "failed", Message: "original"},
		},
	}}
	clone := cloneContextMessages(messages)
	clone[0].ContentParts[0].ImageURL.URL = "changed"
	clone[0].FileReferences[0].Name = "changed"
	clone[0].FileReferences[0].Anchor.Label = "changed"
	clone[0].FileReferences[0].Anchor.Location["page"] = 9
	clone[0].ToolCalls[0].Function.Name = "changed"
	clone[0].Source.Kind = llm.MessageSourceRuntime
	clone[0].Provenance.Producer = "changed"
	clone[0].ToolResultMetadata.Error.Message = "changed"
	if messages[0].ContentParts[0].ImageURL.URL != "data:image/png;base64,abc" || messages[0].FileReferences[0].Name != "source.png" || messages[0].FileReferences[0].Anchor.Label != "第 2 页" || messages[0].FileReferences[0].Anchor.Location["page"] != 2 || messages[0].ToolCalls[0].Function.Name != "bash_run" || messages[0].Source.Kind != llm.MessageSourceUser || messages[0].Provenance.Producer != "human" || messages[0].ToolResultMetadata.Error.Message != "original" {
		t.Fatalf("context clone mutated runtime message: %#v", messages[0])
	}
}

func BenchmarkLifecycleProjectionReplay(b *testing.B) {
	for _, eventCount := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("events_%d", eventCount), func(b *testing.B) {
			events := make([]turn.TurnEventEnvelope, 0, eventCount)
			now := time.Now().UTC()
			events = append(events,
				turn.TurnEventEnvelope{SessionID: "bench", TurnID: "turn-1", SessionSeq: 1, TurnSeq: 1, EventType: turn.EventTurnStarted, EventVersion: 1, Source: string(turn.TurnSourceHuman), CreatedAt: now, Payload: json.RawMessage(`{"generation":1}`)},
				turn.TurnEventEnvelope{SessionID: "bench", TurnID: "turn-1", StepID: "step-1", SessionSeq: 2, TurnSeq: 2, EventType: turn.EventStepStarted, EventVersion: 1, CreatedAt: now, Payload: json.RawMessage(`{"generation":1}`)},
			)
			for index := 2; index < eventCount; index++ {
				payload := json.RawMessage(fmt.Sprintf(`{"generation":1,"external_fact_id":"fact-%d","external_fact_kind":"bench"}`, index))
				events = append(events, turn.TurnEventEnvelope{
					SessionID: "bench", TurnID: "turn-1", StepID: "step-1", SessionSeq: uint64(index + 1), TurnSeq: uint64(index + 1),
					EventType: turn.EventExternalFactRecorded, EventVersion: 1, CommandID: fmt.Sprintf("fact-%d", index), CreatedAt: now, Payload: payload,
				})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				coordinator := turn.NewTurnCoordinator("bench", "agent-1")
				if err := coordinator.Restore(events); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkLifecycleProjectionLookup(b *testing.B) {
	for _, eventCount := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("events_%d", eventCount), func(b *testing.B) {
			b.StopTimer()
			st, err := store.Open(filepath.Join(b.TempDir(), "projection-bench.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer st.Close()
			seedProjectionEvents(b, st, eventCount-2)
			manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, nil)
			if _, _, _, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1"); err != nil {
				b.Fatal(err)
			}

			b.Run("uncached_replay", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for index := 0; index < b.N; index++ {
					if _, err := manager.loadLifecycleProjectionData(context.Background(), "projection-session", "agent-1"); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("cache_hit", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for index := 0; index < b.N; index++ {
					if _, _, _, err := manager.loadLifecycleProjection(context.Background(), "projection-session", "agent-1"); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
