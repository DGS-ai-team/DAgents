package tools

import (
	"strings"
	"testing"
)

func TestLoadSkillsToolDef_describesWhenToUse(t *testing.T) {
	def := loadSkillsToolDef()
	desc := def.Function.Description
	for _, sub := range []string{
		"整组替换",
		"unload_skills",
		"下一个模型 Step",
	} {
		if !strings.Contains(desc, sub) {
			t.Fatalf("load_skills description missing %q: %s", sub, desc)
		}
	}
	if strings.Contains(desc, "可用 skills（name: description）：") {
		t.Fatalf("load_skills description should not embed the catalog: %s", desc)
	}
	if strings.Contains(desc, "必须先调用") {
		t.Fatalf("global skill-selection rule should stay in system prompt: %s", desc)
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

func TestListAvailableSkillsToolDef_isMetadataOnlyAndBounded(t *testing.T) {
	def := ListAvailableSkillsToolDef()
	if def.Function.Name != "list_available_skills" {
		t.Fatalf("name = %q", def.Function.Name)
	}
	for _, want := range []string{"元数据", "query", "分页", "不读取或返回 SKILL.md 正文", "next_cursor"} {
		if !strings.Contains(def.Function.Description, want) {
			t.Fatalf("description missing %q: %s", want, def.Function.Description)
		}
	}
	properties, ok := def.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", def.Function.Parameters["properties"])
	}
	limit, ok := properties["limit"].(map[string]any)
	if !ok || limit["maximum"] != 20 {
		t.Fatalf("limit schema = %#v", properties["limit"])
	}
	if !IsSkillTool("list_available_skills") {
		t.Fatal("list_available_skills should be routed as a skill tool")
	}
}
