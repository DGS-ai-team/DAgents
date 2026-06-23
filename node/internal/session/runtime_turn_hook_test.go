package session

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// 回归：runTurnStep 持 r.mu 时 RunTurnBeforeCompressPhase 会经 getLoadedSkills 再次抢锁而死锁，表现为 prefilling 永久卡住。
func TestHumanMessageWithCompressionAndHooksDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	hub := stream.NewHub(32, logx.Discard())
	mgr := NewManager("agent-1", hub, &slowMockLLM{delay: 50 * time.Millisecond}, reg, pol, nil, TurnOptions{
		SkillsEnabled:       false,
		CompressionBlocking: 1,
		PluginHooks:         hooks.PluginsConfig{},
	}, logx.Discard())
	defer mgr.Stop()

	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "hello", nil, ""); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	gotDone := false
	for !gotDone {
		select {
		case ev := <-ch:
			if ev.Type == "done" {
				gotDone = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn completion; likely deadlock in runTurnStep before LLM")
		}
	}
}
