package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogListAndMetadata(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha-skill", "---\nname: alpha-skill\ndescription: Alpha helper\n---\nAlpha body\n")
	writeSkill(t, root, "beta-skill", "---\nname: beta-skill\ndescription: Beta\n---\nBeta body\n")

	c := NewCatalog(root, true, 3)
	defs := c.List()
	if len(defs) != 2 {
		t.Fatalf("skills = %+v", defs)
	}

	meta := c.ListMetadata()
	if len(meta) != 2 || meta[0].SkillName != "alpha-skill" || meta[0].Description != "Alpha helper" {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestCatalogSelectByNameAndRender(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")

	c := NewCatalog(root, true, 2)
	def, ok := c.SelectByName("writer")
	if !ok || def.Content != "Write clearly." {
		t.Fatalf("select = %+v ok=%v", def, ok)
	}

	section := c.RenderMetadataSection()
	if section == "" || !strings.Contains(section, "writer") || !strings.Contains(section, "Write docs") {
		t.Fatalf("metadata section = %q", section)
	}

	loaded := c.SetLoadedSkills([]string{"writer", "missing"})
	if len(loaded) != 1 || loaded[0].SkillName != "writer" {
		t.Fatalf("loaded = %+v", loaded)
	}
	body := c.RenderLoadedSection(loaded)
	if body == "" || !strings.Contains(body, "writer") || !strings.Contains(body, "Write clearly") {
		t.Fatalf("loaded section = %q", body)
	}
}

func TestCatalogUnloadAndDisabled(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "---\nname: a\ndescription: A skill\n---\nA\n")
	writeSkill(t, root, "b", "---\nname: b\ndescription: B skill\n---\nB\n")

	c := NewCatalog(root, true, 3)
	loaded := c.SetLoadedSkills([]string{"a", "b"})
	loaded = c.UnloadSkills(loaded, []string{"a"})
	if len(loaded) != 1 || loaded[0].SkillName != "b" {
		t.Fatalf("after unload = %+v", loaded)
	}

	if c2 := NewCatalog(root, false, 3); c2.List() != nil {
		t.Fatal("disabled catalog should return nil")
	}
}

func TestParseFrontmatter(t *testing.T) {
	meta, body := parseFrontmatter("---\nname: demo\ndescription: Demo\n---\nHello skill\n")
	if meta["name"] != "demo" || meta["description"] != "Demo" {
		t.Fatalf("meta = %+v", meta)
	}
	if body != "Hello skill" {
		t.Fatalf("body = %q", body)
	}
}

func TestReadSkillUsesDirectoryWhenNameMissing(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "legacy-skill", "---\ndescription: Legacy\n---\nBody\n")

	c := NewCatalog(root, true, 3)
	def, ok := c.SelectByName("legacy-skill")
	if !ok || def.SkillName != "legacy-skill" || def.Description != "Legacy" {
		t.Fatalf("def = %+v ok=%v", def, ok)
	}
}
