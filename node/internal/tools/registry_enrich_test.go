package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

func TestDefinitions_loadSkillsIncludesCatalogMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "writer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: writer\ndescription: Write docs\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetSkillsCatalog(skills.NewCatalog(root, true, 3))
	if err := reg.SetBuiltinEnabled([]string{"load_skills"}); err != nil {
		t.Fatal(err)
	}

	var desc string
	for _, def := range reg.Definitions() {
		if def.Function.Name == "load_skills" {
			desc = def.Function.Description
			break
		}
	}
	if desc == "" {
		t.Fatal("load_skills not in definitions")
	}
	if !containsAll(desc, "Write docs", "可用 skills") {
		t.Fatalf("description = %q", desc)
	}
}

func TestDefinitions_withoutCatalogNoSkillsAppend(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled([]string{"load_skills"}); err != nil {
		t.Fatal(err)
	}
	for _, def := range reg.Definitions() {
		if def.Function.Name == "load_skills" && containsAll(def.Function.Description, "可用 skills（name") {
			t.Fatalf("unexpected catalog block: %q", def.Function.Description)
		}
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !contains(text, part) {
			return false
		}
	}
	return true
}

func contains(text, sub string) bool {
	return len(sub) == 0 || (len(text) >= len(sub) && indexOf(text, sub) >= 0)
}

func indexOf(text, sub string) int {
	for i := 0; i+len(sub) <= len(text); i++ {
		if text[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
