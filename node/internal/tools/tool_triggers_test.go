package tools

import "testing"

func TestTriggerConditionSchemaIsStructured(t *testing.T) {
	schema := triggerConditionSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties=%#v", schema["properties"])
	}
	for _, name := range []string{"interval_seconds", "fire_at", "schedule", "cmd"} {
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
