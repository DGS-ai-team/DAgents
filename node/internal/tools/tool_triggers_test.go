package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func TestTriggerToolDefsExposeStructuredConditionSchema(t *testing.T) {
	for _, def := range []ToolDef{triggerCreateToolDef(), triggerUpdateToolDef()} {
		props, ok := def.Function.Parameters["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s properties = %#v", def.Function.Name, def.Function.Parameters["properties"])
		}
		condition, ok := props["condition"].(map[string]any)
		if !ok {
			t.Fatalf("%s condition = %#v", def.Function.Name, props["condition"])
		}
		if condition["type"] != "object" || condition["additionalProperties"] != false {
			t.Fatalf("%s condition schema = %#v", def.Function.Name, condition)
		}
		conditionProps, ok := condition["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s condition properties = %#v", def.Function.Name, condition["properties"])
		}
		for _, name := range []string{"interval_seconds", "fire_at", "schedule"} {
			if _, ok := conditionProps[name]; !ok {
				t.Fatalf("%s condition missing %q: %#v", def.Function.Name, name, conditionProps)
			}
		}
	}
}

func TestTriggerConditionSchemaIsStructured(t *testing.T) {
	schema := triggerConditionSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties=%#v", schema["properties"])
	}
	for _, name := range []string{"interval_seconds", "fire_at", "schedule"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("missing condition property %q", name)
		}
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties=%#v", schema["additionalProperties"])
	}
	schedule, ok := properties["schedule"].(map[string]any)
	if !ok || schedule["additionalProperties"] != false {
		t.Fatalf("schedule=%#v", properties["schedule"])
	}
}

func TestTriggerCreateUsesScopedPrincipalAndAuditMetadata(t *testing.T) {
	st, err := triggers.OpenStore(t.TempDir()+"/triggers.json", 20)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetTriggerRuntime(st, nil, "agent-a")
	out, err := reg.execTriggerCreate(t.Context(), json.RawMessage(`{"name":"created","task_template":"private","condition":{"interval_seconds":60}}`))
	if err != nil || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("create=%s err=%v", out, err)
	}
	list := st.ListAuthorized(triggers.Principal{Kind: "agent", AgentID: "agent-a"})
	if len(list) != 1 || list[0].OwnerAgentID != "agent-a" || list[0].Controller != "user" || list[0].CreatedBy != "agent-a" {
		t.Fatalf("scoped definition=%+v", list)
	}
}

func TestTriggerToolsUseOwnerAuthorizationAndDefaultRevisionCAS(t *testing.T) {
	st, err := triggers.OpenStore(t.TempDir()+"/triggers.json", 20)
	if err != nil {
		t.Fatal(err)
	}
	d, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "owned", TaskTemplate: "x", TargetAgentID: "a", Condition: map[string]any{"interval_seconds": 60}}, "a", time.Now())
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
	reg.SetTriggerRuntime(st, nil, "a")
	updated, err := reg.execTriggerUpdate(t.Context(), json.RawMessage(`{"trigger_id":"`+d.TriggerID+`","name":"renamed"}`))
	if err != nil || !strings.Contains(updated, `"ok":true`) {
		t.Fatalf("owner update=%s err=%v", updated, err)
	}
	reg.SetTriggerRuntime(st, nil, "b")
	other, err := reg.execTriggerUpdate(t.Context(), json.RawMessage(`{"trigger_id":"`+d.TriggerID+`","name":"hijack"}`))
	if err != nil || !strings.Contains(other, `"trigger not found"`) {
		t.Fatalf("cross-owner update=%s err=%v", other, err)
	}
	reg.SetTriggerRuntime(st, nil, "a")
	cur, err := st.GetAuthorized(triggers.Principal{Kind: "agent", AgentID: "a"}, d.TriggerID)
	if err != nil {
		t.Fatal(err)
	}
	stale := cur.Revision
	name := "changed"
	if _, err = st.UpdateAuthorized(triggers.Principal{Kind: "agent", AgentID: "a"}, d.TriggerID, stale, triggers.UpdatePatch{Name: &name}, time.Now()); err != nil {
		t.Fatal(err)
	}
	cas, err := reg.execTriggerUpdate(t.Context(), json.RawMessage(`{"trigger_id":"`+d.TriggerID+`","revision":1,"name":"stale"}`))
	if err != nil || !strings.Contains(cas, "revision conflict") {
		t.Fatalf("stale default revision=%s err=%v", cas, err)
	}
}
