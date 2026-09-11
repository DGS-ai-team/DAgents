package session

import (
	"context"
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
}
