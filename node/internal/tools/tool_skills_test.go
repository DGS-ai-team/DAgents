package tools

import (
	"strings"
	"testing"
)

func TestLoadSkillsToolDef_describesWhenToUse(t *testing.T) {
	def := loadSkillsToolDef()
	desc := def.Function.Description
	for _, sub := range []string{
		"必须先调用",
		"description",
		"整组替换",
		"unload_skills",
	} {
		if !strings.Contains(desc, sub) {
			t.Fatalf("load_skills description missing %q: %s", sub, desc)
		}
	}
	params := def.Function.Parameters
	skillNames, _ := params["properties"].(map[string]any)["skill_names"].(map[string]any)
	paramDesc, _ := skillNames["description"].(string)
	if !strings.Contains(paramDesc, "skill_name") {
		t.Fatalf("skill_names param description = %q", paramDesc)
	}
	if strings.Contains(desc, "可用 skills（name: description）：") {
		t.Fatalf("load_skills description should not embed the catalog: %s", desc)
	}
}
