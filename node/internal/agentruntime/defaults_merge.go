package agentruntime

// MergeDefaults 深合并模板 defaults 与创建请求覆盖项（仅 map 递归，标量直接覆盖）。
func MergeDefaults(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		ov, ok := v.(map[string]any)
		if !ok {
			out[k] = v
			continue
		}
		var bv map[string]any
		if existing, ok := out[k].(map[string]any); ok {
			bv = existing
		}
		out[k] = MergeDefaults(bv, ov)
	}
	return out
}
