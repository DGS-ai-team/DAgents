package session

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func testPolicyRuntime(agentID, sessionID string, engine *policy.Engine) *runtime {
	return &runtime{
		session: Session{ID: sessionID, AgentID: agentID},
		orch:    turn.NewOrchestrator(agentID, ".", nil, &llm.MockClient{}, nil, engine, turn.SkillAccess{}, nil, nil, hooks.RuntimeConfig{}, nil),
	}
}

func orchestratorPolicyPointer(rt *runtime) uintptr {
	return reflect.ValueOf(rt.orch).Elem().FieldByName("policy").Pointer()
}

func orchestratorPolicy(rt *runtime) *policy.Engine {
	field := reflect.ValueOf(rt.orch).Elem().FieldByName("policy")
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface().(*policy.Engine)
}

func TestSetAgentPolicyRefreshesAllOwnedSessions(t *testing.T) {
	old := policy.NewDefaultEngine()
	mgr := &Manager{sessions: map[string]*runtime{
		"main":  testPolicyRuntime("agent-a", "main", old),
		"goal":  testPolicyRuntime("agent-a", "goal", old),
		"other": testPolicyRuntime("agent-b", "other", old),
	}}
	next := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeNever}})
	mgr.SetAgentPolicy("agent-a", next)
	for _, id := range []string{"main", "goal"} {
		if got := orchestratorPolicy(mgr.sessions[id]).DecideTool("write_file", nil); got != policy.ActionAuto {
			t.Fatalf("%s was not auto-authorized: %s", id, got)
		}
	}
	if got := orchestratorPolicyPointer(mgr.sessions["main"]); got != reflect.ValueOf(next).Pointer() {
		t.Fatalf("main policy pointer=%x want=%x", got, reflect.ValueOf(next).Pointer())
	}
	if got := orchestratorPolicyPointer(mgr.sessions["goal"]); got != reflect.ValueOf(next).Pointer() {
		t.Fatalf("goal policy pointer=%x want=%x", got, reflect.ValueOf(next).Pointer())
	}
	if got := orchestratorPolicyPointer(mgr.sessions["other"]); got == reflect.ValueOf(next).Pointer() {
		t.Fatal("other Agent policy was unexpectedly changed")
	}
	updated := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeRule}})
	mgr.SetAgentPolicy("agent-a", updated)
	if got := orchestratorPolicyPointer(mgr.sessions["main"]); got != reflect.ValueOf(updated).Pointer() {
		t.Fatal("main session did not receive updated policy")
	}
	if got := orchestratorPolicyPointer(mgr.sessions["goal"]); got != reflect.ValueOf(updated).Pointer() {
		t.Fatal("goal session did not receive updated policy")
	}
	if got := orchestratorPolicyPointer(mgr.sessions["other"]); got == reflect.ValueOf(updated).Pointer() {
		t.Fatal("other Agent changed during policy update")
	}
	for _, id := range []string{"main", "goal"} {
		if got := orchestratorPolicy(mgr.sessions[id]).DecideTool("write_file", nil); got != policy.ActionRequireApproval {
			t.Fatalf("%s did not require approval under updated policy: %s", id, got)
		}
	}
}
