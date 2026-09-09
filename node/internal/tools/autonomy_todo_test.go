package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
)

func TestAutonomyTodoToolsAreBoundAndCAS(t *testing.T) {
	store, err := autonomy.Open(t.TempDir() + "/autonomy.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	r.SetAgentID("auto-a")
	r.SetAutonomyRuntime(true, nil, nil)
	r.SetAutonomyTodoStore(store)
	if _, err := r.Execute(context.Background(), "todo_create", `{"call_purpose":"plan","text":"ship"}`); err != nil {
		t.Fatal(err)
	}
	// The turn router strips the display-only call_purpose before dispatch.
	out, err := r.Execute(context.Background(), "todo_list", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var todos []autonomy.Todo
	if err := json.Unmarshal([]byte(out), &todos); err != nil || len(todos) != 1 {
		t.Fatalf("todos=%s err=%v", out, err)
	}
	if _, err := r.Execute(context.Background(), "todo_update", `{"call_purpose":"finish","id":"`+todos[0].ID+`","expected_revision":1,"status":"completed"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), "todo_update", `{"call_purpose":"stale","id":"`+todos[0].ID+`","expected_revision":1,"status":"pending"}`); err == nil {
		t.Fatal("stale CAS accepted")
	}
	if _, err := r.Execute(context.Background(), "todo_delete", `{"call_purpose":"remove","id":"`+todos[0].ID+`","expected_revision":2}`); err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateTodo("auto-a", "keep text")
	if err != nil {
		t.Fatal(err)
	}
	otherAuto, err := NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	otherAuto.SetAgentID("auto-b")
	otherAuto.SetAutonomyRuntime(true, nil, nil)
	otherAuto.SetAutonomyTodoStore(store)
	for _, op := range []string{"todo_update", "todo_delete"} {
		args := `{"call_purpose":"cross","id":"` + second.ID + `","expected_revision":1,"status":"completed"}`
		if op == "todo_delete" {
			args = `{"call_purpose":"cross","id":"` + second.ID + `","expected_revision":1}`
		}
		if _, err := otherAuto.Execute(context.Background(), op, args); err == nil {
			t.Fatalf("cross-agent %s accepted", op)
		}
	}
	kept, ok := store.GetTodo("auto-a", second.ID)
	if !ok || kept.Text != "keep text" || kept.Status != "pending" {
		t.Fatalf("cross-agent changed todo: %+v", kept)
	}
	if _, err := r.Execute(context.Background(), "todo_update", `{"call_purpose":"status","id":"`+second.ID+`","expected_revision":1,"status":"in_progress"}`); err != nil {
		t.Fatal(err)
	}
	kept, _ = store.GetTodo("auto-a", second.ID)
	if kept.Text != "keep text" || kept.Status != "in_progress" {
		t.Fatalf("status update lost text: %+v", kept)
	}
	if _, err := r.Execute(context.Background(), "todo_update", `{"call_purpose":"text","id":"`+second.ID+`","expected_revision":2,"text":"changed"}`); err != nil {
		t.Fatal(err)
	}
	kept, _ = store.GetTodo("auto-a", second.ID)
	if kept.Text != "changed" || kept.Status != "in_progress" {
		t.Fatalf("text update lost status: %+v", kept)
	}
	beforeInvalid, ok := store.GetTodo("auto-a", second.ID)
	if !ok {
		t.Fatal("todo disappeared before invalid-input checks")
	}
	for _, raw := range []string{
		`{"call_purpose":"bad"}`,
		`{"call_purpose":"bad","text":3}`,
		`{"call_purpose":"bad","text":null}`,
		`{"call_purpose":"bad","text":"x"}{}`,
	} {
		if _, err := r.Execute(context.Background(), "todo_create", raw); err == nil {
			t.Fatalf("accepted invalid create %s", raw)
		}
	}
	for _, raw := range []string{
		`{"call_purpose":"bad","id":"` + second.ID + `","expected_revision":1,"status":3}`,
		`{"call_purpose":"bad","id":"` + second.ID + `","expected_revision":1,"status":null}`,
		`{"call_purpose":"bad","id":"` + second.ID + `","expected_revision":1,"text":3}`,
		`{"call_purpose":"bad","id":"` + second.ID + `","expected_revision":1,"text":null}`,
		`{"call_purpose":"bad","id":"` + second.ID + `","expected_revision":1}{}`,
	} {
		if _, err := r.Execute(context.Background(), "todo_update", raw); err == nil {
			t.Fatalf("accepted invalid update %s", raw)
		}
	}
	afterInvalid, ok := store.GetTodo("auto-a", second.ID)
	if !ok || afterInvalid != beforeInvalid {
		t.Fatalf("invalid input mutated todo: before=%+v after=%+v", beforeInvalid, afterInvalid)
	}
	if _, err := r.Execute(context.Background(), "todo_create", `{"call_purpose":"bad","agent_id":"auto-b","text":"x"}`); err == nil {
		t.Fatal("model supplied agent_id accepted")
	}

	other, err := NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	other.SetAgentID("normal")
	other.SetAutonomyRuntime(false, nil, nil)
	other.SetAutonomyTodoStore(store)
	for _, d := range other.Definitions() {
		if strings.HasPrefix(d.Function.Name, "todo_") {
			t.Fatalf("ordinary runtime exposed %s", d.Function.Name)
		}
	}
	if _, err := other.Execute(context.Background(), "todo_list", `{"call_purpose":"inspect"}`); err == nil {
		t.Fatal("ordinary runtime exposed todo")
	}
}
