package tools

import "github.com/DGS-ai-team/DAgents/node/internal/skills"

// SetSkillsCatalog 注入 skills 目录；启用 load_skills 时在 Definitions 中附加 available skills 列表。
func (r *Registry) SetSkillsCatalog(c *skills.Catalog) {
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
				out[i].Function.Description += "\n\n可用 skills（name: description）：\n" + meta
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
