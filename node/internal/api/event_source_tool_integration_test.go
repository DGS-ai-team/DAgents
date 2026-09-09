package api

import (
	"context"
	"encoding/json"
	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/events"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"testing"
)

func TestAgentRuntimeEventSourceListIsOwnerScoped(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	defer srv.Close()
	if err := srv.eventStore.RegisterSource(events.SourceRegistration{SourceID: "a", OwnerAgentID: "auto-a", Revision: 1, Root: t.TempDir(), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := srv.eventStore.RegisterSource(events.SourceRegistration{SourceID: "b", OwnerAgentID: "auto-b", Revision: 1, Root: t.TempDir(), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	built, err := agentruntime.Build(agentruntime.BuildParams{NodeCFG: cfg, BaseTurn: srv.sessions.DefaultTurnOptions(), AgentID: "auto-a", Snapshot: agentruntime.Snapshot{AgentType: "auto", Defaults: map[string]any{"tools": map[string]any{"enabled_groups": []any{"triggers"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Close()
	srv.attachNodeRuntimeDeps(built.Registry, "auto-a")
	out, err := built.Registry.Execute(context.Background(), "event_source_list", `{"call_purpose":"test"}`)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err = json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["source_id"] != "a" {
		t.Fatalf("rows=%s", out)
	}
}
