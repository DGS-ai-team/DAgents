package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// queueRestoreClient blocks after the restored human input is handed to the
// model. This leaves the input in the durable in-flight slot for inspection.
type queueRestoreClient struct{ started chan struct{} }

func (c *queueRestoreClient) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	select {
	case <-c.started:
	default:
		close(c.started)
	}
	<-ctx.Done()
	return llm.ChatResult{}, ctx.Err()
}
func (*queueRestoreClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*queueRestoreClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestDreamingRestoreFailsAttemptWithoutDroppingQueuedHumanInput(t *testing.T) {
	client := &queueRestoreClient{started: make(chan struct{})}
	mgr, reg, st, dbPath, sessionID, _ := dreamingResumeFixture(t, client, policy.ModeNever)
	mgr.Stop()

	record, err := st.Load(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	mailbox := NewInputBox()
	if _, err := mailbox.Append(InputKindUser, queue.Envelope{Content: "queued human must survive"}); err != nil {
		t.Fatal(err)
	}
	attempt, err := json.Marshal(DreamingAttempt{
		AgentID: "agent-1", SessionID: sessionID, TurnID: "orphan-dreaming-turn",
		MaxToolRounds: 2, State: DreamingAttemptWaiting, Boundary: "opaque-boundary",
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	record.RuntimeState.InputBoxState = mailbox.Snapshot()
	record.RuntimeState.DreamingAttempt = attempt
	if err := st.Save(context.Background(), *record); err != nil {
		t.Fatal(err)
	}
	mgr.Stop()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedStore, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedStore.Close()
	reopenedClient := &queueRestoreClient{started: make(chan struct{})}
	reopened := NewManager("agent-1", stream.NewHub(16, logx.Discard()), reopenedClient, reg, policy.NewDefaultEngine(), reopenedStore, TurnOptions{AutoAgent: true}, logx.Discard())
	defer reopened.Stop()
	if _, _, err := reopened.CreateWithOptionsAndLLM(sessionID, TurnOptions{AutoAgent: true, WorkspaceRoot: reg.WorkspaceRoot()}, reg, nil, reopenedClient, "agent-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reopenedClient.started:
	case <-time.After(5 * time.Second):
		t.Fatal("restored human input was not delivered to the model")
	}
	restoredAttempt, found, err := reopened.GetDreamingAttempt(sessionID)
	if err != nil || !found || restoredAttempt.State != DreamingAttemptFailed {
		t.Fatalf("orphan dreaming attempt was not failed: found=%v err=%v attempt=%+v", found, err, restoredAttempt)
	}
	rt := reopened.getRuntime(sessionID)
	if rt == nil {
		t.Fatal("restored runtime missing")
	}
	inFlight, ok := rt.inputBox.InFlight()
	if !ok || inFlight.Env.Content != "queued human must survive" {
		t.Fatalf("queued human input was lost during dreaming restore: ok=%v record=%+v", ok, inFlight)
	}
}
