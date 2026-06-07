package childagent

import "testing"

func TestParseCreateInputSkillNames(t *testing.T) {
	input, err := parseCreateInput(`{"task":"scan logs","purpose":"ops","skill_names":["writer","reviewer"]}`, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(input.SkillNames) != 2 || input.SkillNames[0] != "writer" || input.SkillNames[1] != "reviewer" {
		t.Fatalf("SkillNames = %#v", input.SkillNames)
	}
}
