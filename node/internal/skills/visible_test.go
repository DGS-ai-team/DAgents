package skills

import "testing"

func TestCatalogRestrictVisible(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "---\nname: alpha\ndescription: A\n---\nA\n")
	writeSkill(t, root, "beta", "---\nname: beta\ndescription: B\n---\nB\n")
	writeSkill(t, root, "gamma", "---\nname: gamma\ndescription: G\n---\nG\n")

	c := NewCatalog(root, true, 3).RestrictVisible([]string{"alpha", "gamma"})
	defs := c.List()
	if len(defs) != 2 {
		t.Fatalf("list = %+v", defs)
	}
	meta := c.ListMetadata()
	if len(meta) != 2 || meta[0].SkillName != "alpha" || meta[1].SkillName != "gamma" {
		t.Fatalf("meta = %+v", meta)
	}
	if _, ok := c.SelectByName("beta"); ok {
		t.Fatal("beta should be hidden")
	}
	if _, ok := c.SelectByName("alpha"); !ok {
		t.Fatal("alpha should be visible")
	}
	loaded := c.SetLoadedSkills([]string{"alpha", "beta", "gamma"})
	if len(loaded) != 2 {
		t.Fatalf("loaded = %+v", loaded)
	}
	detailed := c.SetLoadedSkillsDetailed([]string{"alpha", "beta"})
	if len(detailed.Loaded) != 1 || len(detailed.Rejected) != 1 || detailed.Rejected[0].Reason != "not_visible" {
		t.Fatalf("detailed = %+v", detailed)
	}
}

func TestCatalogRestrictVisibleEmptyMeansNone(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "---\nname: alpha\ndescription: A\n---\nA\n")
	c := NewCatalog(root, true, 3).RestrictVisible([]string{})
	if len(c.List()) != 0 {
		t.Fatal("empty allowlist should hide all")
	}
	if _, ok := c.SelectByName("alpha"); ok {
		t.Fatal("alpha should be hidden")
	}
}

func TestCatalogRestrictVisibleNilMeansAll(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "---\nname: alpha\ndescription: A\n---\nA\n")
	c := NewCatalog(root, true, 3).RestrictVisible([]string{"alpha"})
	c.RestrictVisible(nil)
	if len(c.List()) != 1 {
		t.Fatal("nil restrict should restore all")
	}
}
