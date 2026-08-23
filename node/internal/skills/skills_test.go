package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
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

func TestEstimateCatalogStats_metadataOnlyForPromptCatalog(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "---\nname: alpha\ndescription: short desc\n---\n"+strings.Repeat("x", 16004))
	c := NewCatalog(root, true, 3)

	stats := c.EstimateCatalogStats()
	if stats.MetadataTokens >= 4000 {
		t.Fatalf("metadata tokens = %d, want << 4000 (body must not be counted)", stats.MetadataTokens)
	}
	if stats.MaxBodyTokens < 4000 {
		t.Fatalf("max body tokens = %d, want >= 4000", stats.MaxBodyTokens)
	}
	if !stats.ExceedsBloatThreshold(CatalogBloatTokenThreshold) {
		t.Fatal("expected bloat from fat SKILL body")
	}
	if stats.BloatDisplayTokens() != stats.MaxBodyTokens {
		t.Fatalf("display = %d maxBody = %d", stats.BloatDisplayTokens(), stats.MaxBodyTokens)
	}
}

func TestEstimateCatalogStats_metadataMatchesRenderSection(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "---\nname: a\ndescription: Alpha helper\n---\nBody\n")
	writeSkill(t, root, "b", "---\nname: b\ndescription: Beta\n---\nBody\n")

	c := NewCatalog(root, true, 3)
	stats := c.EstimateCatalogStats()
	meta := c.RenderMetadataSection()
	want := tokens.EstimateInt(LoadSkillsMetadataPrefix + meta)
	if stats.MetadataTokens != want {
		t.Fatalf("metadata tokens = %d want %d", stats.MetadataTokens, want)
	}
}

func TestEstimateCatalogMetadataTokens(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "---\nname: alpha\ndescription: desc-a\n---\n"+strings.Repeat("x", 16000))
	c := NewCatalog(root, true, 3)
	if got := c.EstimateCatalogMetadataTokens(); got >= 4000 {
		t.Fatalf("EstimateCatalogMetadataTokens = %d, want metadata-only << 4000", got)
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
	if defs[0].Content != "" || defs[1].Content != "" {
		t.Fatalf("catalog listing should defer bodies: %+v", defs)
	}
}

func TestCatalogSelectLoadsBodyAndKeepsMetadataCatalogSmall(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")
	c := NewCatalog(root, true, 2)

	if defs := c.List(); len(defs) != 1 || defs[0].Content != "" {
		t.Fatalf("List should return metadata only: %+v", defs)
	}
	def, ok := c.SelectByName("writer")
	if !ok || def.Content != "Write clearly." || def.DirectoryName != "writer" {
		t.Fatalf("SelectByName = %+v ok=%v", def, ok)
	}
	if defs := c.List(); len(defs) != 1 || defs[0].Content != "" {
		t.Fatalf("metadata cache should remain body-free: %+v", defs)
	}
}

func TestCatalogTimingSeparatesMetadataBoundaryBodyAndTokenCosts(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")
	catalog := NewCatalog(root, true, 2)

	_ = catalog.List()
	beforeView := catalog.TimingSnapshot()
	if beforeView.MetadataScanCount == 0 || beforeView.BodyReadCount != 0 || beforeView.BoundaryDigestCount != 0 {
		t.Fatalf("metadata timing = %+v", beforeView)
	}

	view := catalog.NewTurnView()
	afterView := view.TimingSnapshot()
	if afterView.BoundaryDigestCount != 1 || afterView.BodyReadCount != 0 {
		t.Fatalf("turn boundary timing = %+v", afterView)
	}
	if defs := view.List(); len(defs) != 1 || defs[0].Content != "" {
		t.Fatalf("frozen metadata should remain body-free: %+v", defs)
	}

	loaded := view.SetLoadedSkills([]string{"writer"})
	if len(loaded) != 1 {
		t.Fatalf("loaded = %+v", loaded)
	}
	loadedTiming := view.TimingSnapshot()
	if loadedTiming.BodyReadCount != 1 || loadedTiming.BodyReadBytes == 0 {
		t.Fatalf("body timing = %+v", loadedTiming)
	}

	_ = view.EstimateCatalogStats()
	estimateTiming := view.TimingSnapshot()
	if estimateTiming.TokenEstimateCount != 1 || estimateTiming.BodyCacheHitCount == 0 {
		t.Fatalf("estimate timing = %+v", estimateTiming)
	}
}

func TestSetLoadedSkillsDetailedReportsCapacityAndMissing(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nBody\n")
	writeSkill(t, root, "reviewer", "---\nname: reviewer\ndescription: Review docs\n---\nBody\n")
	c := NewCatalog(root, true, 1)

	result := c.SetLoadedSkillsDetailed([]string{"writer", "missing", "reviewer"})
	if len(result.Requested) != 3 || len(result.Loaded) != 1 || result.Loaded[0].SkillName != "writer" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Rejected) != 2 || result.Rejected[0].Reason != "not_found" || result.Rejected[1].Reason != "max_in_prompt" {
		t.Fatalf("rejected = %+v", result.Rejected)
	}
}

func TestCatalogFrontmatterNameUsesDirectoryForBodyAndVisibility(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "writer-dir", "---\nname: writer\ndescription: Write docs\n---\nBody\n")
	c := NewCatalog(root, true, 2).RestrictVisible([]string{"writer-dir"})

	def, ok := c.SelectByName("writer")
	if !ok || def.DirectoryName != "writer-dir" || def.Content != "Body" {
		t.Fatalf("SelectByName = %+v ok=%v", def, ok)
	}
	loaded := c.SetLoadedSkills([]string{"writer"})
	if len(loaded) != 1 || loaded[0].DirectoryName != "writer-dir" {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func TestCatalogSetLoadedSkillsDeduplicatesLogicalAndDirectoryAliases(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "writer-dir", "---\nname: writer\ndescription: Write docs\n---\nBody\n")
	c := NewCatalog(root, true, 2)

	result := c.SetLoadedSkillsDetailed([]string{"writer", "writer-dir"})
	if len(result.Loaded) != 1 || result.Loaded[0].DirectoryName != "writer-dir" {
		t.Fatalf("loaded = %+v", result.Loaded)
	}
	if len(result.Rejected) != 1 || result.Rejected[0].Name != "writer-dir" || result.Rejected[0].Reason != "duplicate" {
		t.Fatalf("rejected = %+v", result.Rejected)
	}
}

func TestCatalogRejectsAmbiguousLogicalName(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "writer-a", "---\nname: writer\ndescription: A\n---\nA\n")
	writeSkill(t, root, "writer-b", "---\nname: writer\ndescription: B\n---\nB\n")
	c := NewCatalog(root, true, 2)

	result := c.SetLoadedSkillsDetailed([]string{"writer"})
	if len(result.Loaded) != 0 {
		t.Fatalf("ambiguous name should not load: %+v", result.Loaded)
	}
	if len(result.Rejected) != 1 || result.Rejected[0].Reason != "ambiguous" {
		t.Fatalf("rejected = %+v", result.Rejected)
	}
	section := c.RenderMetadataSection()
	if !strings.Contains(section, "writer（目录：writer-a）") || !strings.Contains(section, "writer（目录：writer-b）") {
		t.Fatalf("ambiguous catalog should expose directory disambiguators: %q", section)
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
	contents := c.ReadLoadedSkillContents(loaded)
	if len(contents) != 1 || contents[0].SkillName != "writer" || contents[0].Content != "Write clearly." {
		t.Fatalf("loaded contents = %+v", contents)
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

func TestParseFrontmatter_crlfAndDescriptionWithColon(t *testing.T) {
	raw := "---\r\nname: write-skill\r\ndescription: 用途：编写 SKILL.md\r\n---\r\nBody\r\n"
	meta, body := parseFrontmatter(raw)
	if meta["name"] != "write-skill" {
		t.Fatalf("name = %#v", meta["name"])
	}
	if meta["description"] != "用途：编写 SKILL.md" {
		t.Fatalf("description = %#v", meta["description"])
	}
	if body != "Body" {
		t.Fatalf("body = %q", body)
	}
}

func repoSkillsRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "packaging", "runtime", "skills")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("packaging/runtime/skills not found from test cwd")
		}
		dir = parent
	}
}

func TestCatalogProductionLayout_writeSkillDescription(t *testing.T) {
	fsRoot := t.TempDir()
	skillsRoot := filepath.Join(fsRoot, "skills")
	writeSkill(t, skillsRoot, "write-skill", "---\nname: write-skill\ndescription: Packaged helper\n---\nBody\n")

	c := NewCatalog(skillsRoot, true, 3)
	meta := c.ListMetadata()
	if len(meta) != 1 || meta[0].Description != "Packaged helper" {
		t.Fatalf("ListMetadata = %+v", meta)
	}
	section := c.RenderMetadataSection()
	if !strings.Contains(section, "write-skill: Packaged helper") {
		t.Fatalf("RenderMetadataSection = %q", section)
	}
}

func TestWriteSkillPackagingDescription(t *testing.T) {
	root := repoSkillsRoot(t)
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
	section := c.RenderMetadataSection()
	if !strings.Contains(section, "write-skill:") || !strings.Contains(section, found.Description) {
		t.Fatalf("RenderMetadataSection missing write-skill description: %q", section)
	}
	stats := c.EstimateCatalogStats()
	if stats.MetadataTokens <= 0 {
		t.Fatalf("metadata tokens = %d", stats.MetadataTokens)
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

func TestCatalogTurnViewFreezesMetadataAndRejectsChangedBody(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "writer", "SKILL.md")
	writeSkill(t, root, "writer", "---\nname: writer\ndescription: v1\n---\n123456\n")
	catalog := NewCatalog(root, true, 2)
	view := catalog.NewTurnView()
	if view == nil || view.Revision() == "" {
		t.Fatalf("turn view = %#v", view)
	}

	// Keep the replacement the same size to prove the view does not rely on
	// mtime/size alone for active-Turn freshness.
	if err := os.WriteFile(path, []byte("---\nname: writer\ndescription: v2\n---\n654321\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := view.RenderMetadataSection(); !strings.Contains(got, "v1") || strings.Contains(got, "v2") {
		t.Fatalf("frozen metadata changed: %q", got)
	}
	if _, ok := view.SelectByName("writer"); ok {
		t.Fatal("frozen view should reject a body changed after the Turn boundary")
	}
	result := view.SetLoadedSkillsDetailed([]string{"writer"})
	if len(result.Loaded) != 0 || len(result.Rejected) != 1 || result.Rejected[0].Reason != "catalog_changed" {
		t.Fatalf("changed body result = %+v", result)
	}
}

func TestCatalogTurnViewKeepsAlreadyLoadedBodyStable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "writer", "SKILL.md")
	writeSkill(t, root, "writer", "---\nname: writer\ndescription: docs\n---\nold body\n")
	view := NewCatalog(root, true, 2).NewTurnView()
	loaded := view.SetLoadedSkills([]string{"writer"})
	if len(loaded) != 1 {
		t.Fatalf("loaded = %+v", loaded)
	}
	if err := os.WriteFile(path, []byte("---\nname: writer\ndescription: docs\n---\nnew body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	section := view.RenderLoadedSection(loaded)
	if !strings.Contains(section, "old body") || strings.Contains(section, "new body") {
		t.Fatalf("loaded body was not stable: %q", section)
	}
}

func TestCatalogTurnViewRevisionDetectsSameSizeContentChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "writer", "SKILL.md")
	writeSkill(t, root, "writer", "---\nname: writer\ndescription: docs\n---\nold body\n")
	catalog := NewCatalog(root, true, 2)
	first := catalog.NewTurnView()
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: writer\ndescription: docs\n---\nnew body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	second := catalog.NewTurnView()
	if first.Revision() == second.Revision() {
		t.Fatalf("same-size content change did not change view revision: %q", first.Revision())
	}
}

func TestListAvailableSkillsIsBoundedSearchableAndMetadataOnly(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "b-skill", "---\nname: beta\ndescription: Beta helper\n---\n"+strings.Repeat("body", 100))
	writeSkill(t, root, "a-skill", "---\nname: alpha\ndescription: Alpha helper\n---\nAlpha body\n")
	writeSkill(t, root, "hidden", "---\nname: hidden\ndescription: Hidden\n---\nHidden body\n")
	catalog := NewCatalog(root, true, 2).RestrictVisible([]string{"a-skill", "b-skill"})

	page, err := catalog.ListAvailableSkills("helper", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Skills) != 1 || page.Skills[0].DirectoryName != "a-skill" || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("first page = %+v", page)
	}
	if page.Skills[0].Description != "Alpha helper" {
		t.Fatalf("metadata page = %+v", page.Skills)
	}
	second, err := catalog.ListAvailableSkills("helper", 1, page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Skills) != 1 || second.Skills[0].DirectoryName != "b-skill" || second.HasMore || second.NextCursor != "" {
		t.Fatalf("second page = %+v", second)
	}
	all, err := catalog.ListAvailableSkills("", 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Skills) != 2 || all.Skills[0].DirectoryName != "a-skill" || all.Skills[1].DirectoryName != "b-skill" {
		t.Fatalf("visible sorted skills = %+v", all.Skills)
	}
	for _, item := range all.Skills {
		if item.SkillName == "hidden" || strings.Contains(item.Description, "body") {
			t.Fatalf("list leaked hidden/body content: %+v", all.Skills)
		}
	}
}

func TestListAvailableSkillsRejectsInvalidCursor(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "---\nname: a\ndescription: A\n---\nA\n")
	_, err := NewCatalog(root, true, 2).ListAvailableSkills("", 10, "not-a-cursor")
	if err == nil || err.Error() != "invalid_cursor" {
		t.Fatalf("invalid cursor err = %v", err)
	}
}
