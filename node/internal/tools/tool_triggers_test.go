package tools

import "testing"

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
		for _, name := range []string{"interval_seconds", "fire_at", "schedule", "cmd"} {
			if _, ok := conditionProps[name]; !ok {
				t.Fatalf("%s condition missing %q: %#v", def.Function.Name, name, conditionProps)
			}
		}
	}
}
