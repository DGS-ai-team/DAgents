package agenttemplate

import (
	"os"
	"path/filepath"
)

var builtinTemplateCandidates = []string{
	"packaging/agent-templates",
	"../packaging/agent-templates",
	"../../packaging/agent-templates",
	"../../../packaging/agent-templates",
	"../../../../packaging/agent-templates",
}

// ResolveBuiltinDir 在常见相对路径中查找内置模板目录；找不到返回空串。
func ResolveBuiltinDir() string {
	for _, cand := range builtinTemplateCandidates {
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			abs, err := filepath.Abs(cand)
			if err == nil {
				return abs
			}
			return cand
		}
	}
	return ""
}
