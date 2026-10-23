package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestBuildAgentSessionRuntimeKeepsWorkgroupInputsIsolated(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	t.Cleanup(func() { srv.sessions.Stop() })
	rec := &store.AgentRecord{
		AgentID:         "agt-runtime-helper",
		RuntimeRevision: 7,
		ConfigSnapshot:  json.RawMessage(`{"defaults":{"tools":{"enabled_groups":["fs"]}}}`),
	}

	personal, err := srv.buildAgentSessionRuntime(context.Background(), rec, agentSessionRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = personal.Built.Close() })
	workgroup, err := srv.buildAgentSessionRuntime(context.Background(), rec, agentSessionRuntimeOptions{DisableMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workgroup.Built.Close() })

	if personal.Built.Registry == nil || workgroup.Built.Registry == nil || personal.Built.Registry == workgroup.Built.Registry {
		t.Fatal("personal and workgroup runtimes must own distinct tool registries")
	}
	if personal.Built.TurnOptions.MemoryService == nil {
		t.Fatal("personal runtime should retain Agent memory")
	}
	if workgroup.Built.TurnOptions.MemoryService != nil {
		t.Fatal("workgroup runtime must not open the Agent's personal memory")
	}
	if personal.Built.TurnOptions.AgentID != rec.AgentID || workgroup.Built.TurnOptions.AgentID != rec.AgentID ||
		personal.Built.TurnOptions.RuntimeRevision != rec.RuntimeRevision || workgroup.Built.TurnOptions.RuntimeRevision != rec.RuntimeRevision {
		t.Fatalf("shared runtime inputs lost Agent identity/revision: personal=%+v workgroup=%+v", personal.Built.TurnOptions, workgroup.Built.TurnOptions)
	}
}
