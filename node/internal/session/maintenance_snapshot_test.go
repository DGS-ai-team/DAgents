package session

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestProductionSessionWritesCompletedMaintenanceSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := tools.NewRegistry(t.TempDir(), 30)
	mgr := NewManager("agent-1", stream.NewHub(32, logx.Discard()), &llm.MockClient{}, reg, policy.NewDefaultEngine(), st, TurnOptions{SkillsEnabled: false}, logx.Discard())
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "remember this durable input", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, _ := st.ListTurnEvents(context.Background(), s.ID, 0, 200)
		for _, e := range events {
			if e.EventType == turn.EventTurnCompleted {
				goto done
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("turn did not complete")
done:
	items, err := st.ListCompletedTurnSnapshots(context.Background(), "agent-1", 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("snapshots=%+v err=%v", items, err)
	}
	if items[0].Status != "readable" || len(items[0].Messages) < 2 || items[0].Messages[0].Role != "user" || items[0].Messages[0].Content != "remember this durable input" {
		t.Fatalf("snapshot=%+v", items[0])
	}
	var finalAssistantFound bool
	for _, message := range items[0].Messages {
		if message.Role == "assistant" && message.Content == "remember this durable input" {
			finalAssistantFound = true
			break
		}
	}
	if !finalAssistantFound {
		t.Fatalf("completed snapshot is missing the final assistant response: %+v", items[0].Messages)
	}
	if items[0].ConversationRevision == 0 || items[0].CheckpointRevision == 0 {
		t.Fatalf("snapshot lacks committed revision binding: %+v", items[0])
	}
	events, err := st.ListTurnEventsForTurn(context.Background(), s.ID, items[0].TurnID)
	if err != nil || len(events) < 2 {
		t.Fatalf("terminal events=%+v err=%v", events, err)
	}
	var terminal turn.TurnEventEnvelope
	for _, event := range events {
		if event.EventType == turn.EventTurnCompleted {
			terminal = event
		}
	}
	if terminal.ID == 0 || !bytes.Contains(terminal.Payload, []byte(`"conversation_revision"`)) || !bytes.Contains(terminal.Payload, []byte(`"checkpoint_revision"`)) || !bytes.Contains(terminal.Payload, []byte(`"lifecycle_covered_seq":0`)) {
		t.Fatalf("terminal event revision binding missing: %+v", terminal)
	}
	var terminalPayload struct {
		ConversationRevision uint64 `json:"conversation_revision"`
		CheckpointRevision   uint64 `json:"checkpoint_revision"`
		LifecycleCoveredSeq  uint64 `json:"lifecycle_covered_seq"`
	}
	if err := json.Unmarshal(terminal.Payload, &terminalPayload); err != nil {
		t.Fatalf("decode terminal revision payload: %v", err)
	}
	if terminalPayload.ConversationRevision != items[0].ConversationRevision || terminalPayload.CheckpointRevision != items[0].CheckpointRevision || terminalPayload.LifecycleCoveredSeq != 0 {
		t.Fatalf("terminal payload and snapshot revisions differ: payload=%+v snapshot=(%d,%d)", terminalPayload, items[0].ConversationRevision, items[0].CheckpointRevision)
	}
	mgr.Stop()
	st.Close()
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err = reopened.ListCompletedTurnSnapshots(context.Background(), "agent-1", 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("reopened=%+v err=%v", items, err)
	}
	if items[0].ConversationRevision == 0 || items[0].CheckpointRevision == 0 {
		t.Fatalf("reopened snapshot lost revision binding: %+v", items[0])
	}
	var reopenedAssistantFound bool
	for _, message := range items[0].Messages {
		if message.Role == "assistant" && message.Content == "remember this durable input" {
			reopenedAssistantFound = true
			break
		}
	}
	if !reopenedAssistantFound || items[0].ConversationRevision != terminalPayload.ConversationRevision || items[0].CheckpointRevision != terminalPayload.CheckpointRevision {
		t.Fatalf("restart snapshot lost final response/revision binding: %+v terminal=%+v", items[0], terminalPayload)
	}
	// Simulate process restart after the terminal transaction committed but
	// before any post-commit observer/SSE notification could run.
	mgr2 := NewManager("agent-1", stream.NewHub(16, logx.Discard()), &llm.MockClient{}, reg, policy.NewDefaultEngine(), reopened, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr2.Stop()
	if _, _, err := mgr2.Create(s.ID); err != nil {
		t.Fatal(err)
	}
	view, err := mgr2.GetHydrateView(s.ID)
	if err != nil || !view.TurnState.Terminal || view.TurnState.TurnStatus != string(turn.TurnStatusCompleted) {
		t.Fatalf("restart did not recover committed terminal boundary: view=%+v err=%v", view, err)
	}
}
