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

func TestCatalogListEnabledAndMetadata(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha-skill", "---\ndescription: Alpha helper\nenabled: true\n---\nAlpha body\n")
	writeSkill(t, root, "beta-skill", "---\ndescription: Beta\nenabled: false\n---\nHidden\n")

	c := NewCatalog(root, true, 3)
	defs := c.ListEnabled()
	if len(defs) != 1 || defs[0].SkillName != "alpha-skill" {
		t.Fatalf("enabled skills = %+v", defs)
	}

	meta := c.ListMetadata()
	if len(meta) != 1 || meta[0].SkillName != "alpha-skill" || meta[0].Description != "Alpha helper" {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestCatalogSelectByNameAndRender(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "writer", "---\ndescription: Write docs\n---\nWrite clearly.\n")

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
	writeSkill(t, root, "a", "---\n---\nA\n")
	writeSkill(t, root, "b", "---\n---\nB\n")

	c := NewCatalog(root, true, 3)
	loaded := c.SetLoadedSkills([]string{"a", "b"})
	loaded = c.UnloadSkills(loaded, []string{"a"})
	if len(loaded) != 1 || loaded[0].SkillName != "b" {
		t.Fatalf("after unload = %+v", loaded)
	}

	if c2 := NewCatalog(root, false, 3); c2.ListEnabled() != nil {
		t.Fatal("disabled catalog should return nil")
	}
}

func TestParseFrontmatter(t *testing.T) {
	meta, body := parseFrontmatter("---\ndescription: Demo\nenabled: true\n---\nHello skill\n")
	if meta["description"] != "Demo" || meta["enabled"] != true {
		t.Fatalf("meta = %+v", meta)
	}
	if body != "Hello skill" {
		t.Fatalf("body = %q", body)
	}
}
