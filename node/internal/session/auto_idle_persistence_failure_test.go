package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// closeOnAutoIdleClient closes the durable store after the provider has been
// entered, so the lifecycle completion path observes a real persistence error
// after the model has returned a successful auto_idle call.
type closeOnAutoIdleClient struct {
	store    *store.SQLiteStore
	once     sync.Once
	requests int
}

func (c *closeOnAutoIdleClient) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	c.once.Do(func() { _ = c.store.Close() })
	c.requests++
	return llm.ChatResult{
		ToolCalls:    []llm.ToolCall{{ID: "idle-persist-failure", Type: "function", Function: llm.ToolCallFunction{Name: "auto_idle", Arguments: `{}`}}},
		FinishReason: "tool_calls",
	}, nil
}

func (*closeOnAutoIdleClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*closeOnAutoIdleClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestTrustedAutoIdlePersistenceFailureDoesNotPublishSuccess(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/sessions.db")
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	client := &closeOnAutoIdleClient{store: st}
	mgr := NewManager("agent-1", hub, client, reg,
		policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"auto_idle": policy.ModeNever}}),
		st, TurnOptions{
			AutoAgent: true,
			TriggerToolRoundProvider: func(context.Context, string, string, string) (int, bool, error) {
				return 1, true, nil
			},
		}, logx.Discard())
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()
	events := hub.Subscribe(0)
	defer hub.Unsubscribe(events)
	rt := mgr.getRuntime(sess.ID)
	if rt == nil {
		t.Fatal("runtime was not created")
	}
	if _, err := rt.inputBox.Append(InputKindSystemAuto, queue.Envelope{
		RequestType: queue.RequestTypeMessage,
		Content:     "检查是否有工作",
		TriggerID:   "auto-failure",
		DeliveryID:  "delivery-failure",
	}); err != nil {
		t.Fatal(err)
	}
	rt.signalInputBox()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == "turn_finished" {
				if noWork, _ := ev.Data["no_work"].(bool); noWork {
					t.Fatal("persistence failure published successful no_work")
				}
			}
		case <-deadline:
			if client.requests == 0 {
				t.Fatal("provider was not invoked before persistence failure")
			}
			return
		}
	}
}
