package tools

// loadSkillsMetadataPrefix 附在 load_skills description 后的固定前缀（须与 skills.LoadSkillsMetadataPrefix 一致）。
const loadSkillsMetadataPrefix = "\n\n可用 skills（name: description）：\n"

// SkillsCatalogEnricher 提供 load_skills 工具 description 所需的 catalog 元数据段。
type SkillsCatalogEnricher interface {
	RenderMetadataSection() string
}

// skillsCatalogHolder 承载 skills catalog 字段，使 registry.go 无需 import skills。
type skillsCatalogHolder struct {
	skillsCatalog SkillsCatalogEnricher
}

// SetSkillsCatalog 注入 skills 目录；启用 load_skills 时在 Definitions 中附加 available skills 列表。
func (r *Registry) SetSkillsCatalog(c SkillsCatalogEnricher) {
	if r == nil {
		return
	}
	r.skillsCatalog = c
}

func (r *Registry) enrichDefinitions(defs []ToolDef) []ToolDef {
	if r == nil || len(defs) == 0 {
		return defs
	}
	out := make([]ToolDef, len(defs))
	copy(out, defs)
	for i := range out {
		switch out[i].Function.Name {
		case "load_skills":
			if meta := r.skillsMetadataSection(); meta != "" {
				out[i].Function.Description += loadSkillsMetadataPrefix + meta
			}
		}
	}
	return out
}

func (r *Registry) skillsMetadataSection() string {
	if r == nil || r.skillsCatalog == nil {
		return ""
	}
	return r.skillsCatalog.RenderMetadataSection()
}
