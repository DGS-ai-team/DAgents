package agenttemplate

import "strings"

// PromptBodiesFromDefaults 读取 defaults.prompt_context 中的 soul/custom 正文预设。
// 这些字段仅用于创建时种子侧车表，不应作为运行时开关语义。
func PromptBodiesFromDefaults(defaults map[string]any) (soulMD, customMD string) {
	if defaults == nil {
		return "", ""
	}
	raw, ok := defaults["prompt_context"]
	if !ok || raw == nil {
		return "", ""
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return "", ""
	}
	if v, ok := m["soul_md"].(string); ok {
		soulMD = strings.TrimSpace(v)
	}
	if v, ok := m["custom_md"].(string); ok {
		customMD = strings.TrimSpace(v)
	}
	return soulMD, customMD
}

// StripPromptBodiesFromDefaults 从 defaults.prompt_context 移除正文，避免写入 config_snapshot。
// 会就地修改 defaults（若 prompt_context 为 map）。
func StripPromptBodiesFromDefaults(defaults map[string]any) {
	if defaults == nil {
		return
	}
	raw, ok := defaults["prompt_context"]
	if !ok || raw == nil {
		return
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return
	}
	delete(m, "soul_md")
	delete(m, "custom_md")
	delete(m, "user_md")
	delete(m, "long_term_md")
}
