package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestEstimateCatalogTokens(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "---\nname: alpha\ndescription: desc-a\n---\n"+strings.Repeat("x", 16000))
	c := NewCatalog(root, true, 3)
	got := c.EstimateCatalogTokens()
	if got < 4000 {
		t.Fatalf("EstimateCatalogTokens = %d, want >= 4000", got)
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

func TestReadSkillMissingDescriptionNotNilLiteral(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "write-skill", "---\nname: write-skill\n---\nBody\n")

	c := NewCatalog(root, true, 3)
	meta := c.ListMetadata()
	if len(meta) != 1 {
		t.Fatalf("meta = %+v", meta)
	}
	if meta[0].Description != "" {
		t.Fatalf("description = %q, want empty not <nil>", meta[0].Description)
	}
}

func TestCatalogListMtimeCacheInvalidatesOnChange(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "---\nname: a\ndescription: v1\n---\nA\n")
	c := NewCatalog(root, true, 3)

	defs1 := c.List()
	if len(defs1) != 1 || defs1[0].Description != "v1" {
		t.Fatalf("defs1 = %+v", defs1)
	}
	if c.List()[0].Description != "v1" {
		t.Fatal("cache should still serve v1")
	}

	time.Sleep(20 * time.Millisecond)
	writeSkill(t, root, "a", "---\nname: a\ndescription: v2\n---\nA\n")
	defs2 := c.List()
	if len(defs2) != 1 || defs2[0].Description != "v2" {
		t.Fatalf("cache not invalidated: %+v", defs2)
	}
}

func TestCatalogListMtimeCacheInvalidatesOnNewSkill(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "---\nname: a\ndescription: A\n---\nA\n")
	c := NewCatalog(root, true, 3)
	if len(c.List()) != 1 {
		t.Fatal("expected one skill")
	}

	writeSkill(t, root, "b", "---\nname: b\ndescription: B\n---\nB\n")
	defs := c.List()
	if len(defs) != 2 {
		t.Fatalf("expected two skills after add: %+v", defs)
	}
}

func TestWriteSkillPackagingDescription(t *testing.T) {
	root := filepath.Join("..", "..", "..", "packaging", "runtime", "skills")
	c := NewCatalog(root, true, 3)
	var found LoadedSkill
	for _, m := range c.ListMetadata() {
		if m.SkillName == "write-skill" {
			found = m
			break
		}
	}
	if found.SkillName == "" {
		t.Fatal("write-skill not found in catalog")
	}
	if strings.TrimSpace(found.Description) == "" {
		t.Fatalf("write-skill description empty: %#v", found)
	}
}
