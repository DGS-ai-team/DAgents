package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// ToolArgsFingerprint 对 tool 名 + 规范化参数（剥离 call_purpose）计算指纹。
func ToolArgsFingerprint(toolName, argumentsJSON string) string {
	cleaned := tools.ParseToolCallArguments(argumentsJSON)
	canonical := canonicalJSONString(cleaned)
	name := strings.ToLower(strings.TrimSpace(toolName))
	sum := sha256.Sum256([]byte(name + "\x00" + canonical))
	return hex.EncodeToString(sum[:])
}

func canonicalJSONString(rawJSON string) string {
	rawJSON = strings.TrimSpace(rawJSON)
	if rawJSON == "" {
		return "{}"
	}
	var v any
	if err := json.Unmarshal([]byte(rawJSON), &v); err != nil {
		return rawJSON
	}
	b, err := json.Marshal(canonicalizeValue(v))
	if err != nil {
		return rawJSON
	}
	return string(b)
}

func canonicalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(keys))
		for _, k := range keys {
			out[k] = canonicalizeValue(t[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = canonicalizeValue(item)
		}
		return out
	default:
		return v
	}
}
