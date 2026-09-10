package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func TestTriggerToolsOwnerTargetAndControllerBoundaries(t *testing.T) {
	st, err := triggers.OpenStore(t.TempDir()+"/triggers.json", 20)
	if err != nil {
		t.Fatal(err)
	}
	d, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "cross-target", TaskTemplate: "x", TargetAgentID: "b", Condition: map[string]any{"interval_seconds": 60}}, "node", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d.OwnerAgentID, d.Controller, d.ControllerID = "a", "user", "a"
	if _, err = st.CreateTrigger(d); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetTriggerRuntime(st, nil, "b")
	for name, raw := range map[string]string{
		"get":    `{"trigger_id":"` + d.TriggerID + `"}`,
		"update": `{"trigger_id":"` + d.TriggerID + `","name":"hijack"}`,
		"delete": `{"trigger_id":"` + d.TriggerID + `"}`,
	} {
		var got string
		switch name {
		case "get":
			got, _ = reg.execTriggerGet(t.Context(), json.RawMessage(raw))
		case "update":
			got, _ = reg.execTriggerUpdate(t.Context(), json.RawMessage(raw))
		case "delete":
			got, _ = reg.execTriggerDelete(t.Context(), json.RawMessage(raw))
		}
		if !strings.Contains(got, `"trigger not found"`) {
			t.Fatalf("B %s=%s", name, got)
		}
	}
	reg.SetTriggerRuntime(st, nil, "a")
	list, _ := reg.execTriggerList(t.Context(), nil)
	if !strings.Contains(list, d.TriggerID) {
		t.Fatalf("owner list=%s", list)
	}
	got, _ := reg.execTriggerGet(t.Context(), json.RawMessage(`{"trigger_id":"`+d.TriggerID+`"}`))
	if !strings.Contains(got, d.TriggerID) {
		t.Fatalf("owner get=%s", got)
	}
	blocked, _ := reg.execTriggerUpdate(t.Context(), json.RawMessage(`{"trigger_id":"`+d.TriggerID+`","name":"hijack"}`))
	if !strings.Contains(blocked, "trigger target is not this agent") {
		t.Fatalf("cross-target write=%s", blocked)
	}
	cur, _ := st.GetTrigger(d.TriggerID)
	if cur.Name != "cross-target" {
		t.Fatalf("stored trigger changed: %q", cur.Name)
	}
	d.TargetAgentID = "a"
	d.Controller = "user"
	if err = st.ReplaceTrigger(d); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"trigger_id":"` + d.TriggerID + `","revision":0,"name":"zero"}`, `{"trigger_id":"` + d.TriggerID + `","revision":-1,"name":"negative"}`} {
		out, _ := reg.execTriggerUpdate(t.Context(), json.RawMessage(raw))
		if !strings.Contains(out, "revision must be positive") {
			t.Fatalf("invalid revision=%s", out)
		}
	}
	d.Controller = "maintenance"
	if err = st.ReplaceTrigger(d); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{"update": `{"trigger_id":"` + d.TriggerID + `","name":"blocked"}`, "delete": `{"trigger_id":"` + d.TriggerID + `"}`} {
		var out string
		if name == "update" {
			out, _ = reg.execTriggerUpdate(t.Context(), json.RawMessage(raw))
		} else {
			out, _ = reg.execTriggerDelete(t.Context(), json.RawMessage(raw))
		}
		if !strings.Contains(out, "system-managed trigger") {
			t.Fatalf("maintenance %s=%s", name, out)
		}
	}
}
