package setup

// PatchHasBlock 判断 PATCH 是否包含至少一个可写配置块。
func PatchHasBlock(p SettingsPatch) bool {
	return p.LLM != nil || p.Manage != nil || p.Features != nil || p.Compression != nil ||
		p.Runtime != nil || p.Agent != nil || p.ChildAgents != nil || p.Browser != nil ||
		p.Tools != nil || p.Hooks != nil
}

// PatchHasNonLLMBlock 判断是否包含除 LLM 以外的配置块（需写入 config.yaml）。
func PatchHasNonLLMBlock(p SettingsPatch) bool {
	return p.Manage != nil || p.Features != nil || p.Compression != nil ||
		p.Runtime != nil || p.Agent != nil || p.ChildAgents != nil || p.Browser != nil ||
		p.Tools != nil || p.Hooks != nil
}
