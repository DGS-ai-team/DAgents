package toolresult

import (
	"strings"
	"unicode"
)

// 与 DeepSeek 离线估算一致：https://api-docs.deepseek.com/zh-cn/quick_start/token_usage
// 1 个中文字符 ≈ 0.6 token；1 个英文字符 ≈ 0.3 token。
const (
	tokenWeightASCII = 0.3
	tokenWeightCJK   = 0.6
)

// EstimateTokens 按 DeepSeek 文档粗算文本 token 数（非精确分词，以 usage 为准）。
func EstimateTokens(text string) float64 {
	var sum float64
	for _, r := range text {
		sum += tokenWeight(r)
	}
	return sum
}

// TakePrefixForTokenBudget 保留 s 的前缀，使 DeepSeek 粗算 token 不超过 maxTokens。
func TakePrefixForTokenBudget(s string, maxTokens float64) string {
	if maxTokens <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0.0
	for _, r := range s {
		w := tokenWeight(r)
		if used+w > maxTokens && used > 0 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
}

// ClipToTokenBudget 将文本截到 token 预算内（保留前缀）；第二返回值表示是否发生截断。
func ClipToTokenBudget(text string, maxTokens float64) (string, bool) {
	if maxTokens <= 0 || EstimateTokens(text) <= maxTokens {
		return text, false
	}
	return TakePrefixForTokenBudget(text, maxTokens), true
}

func tokenWeight(r rune) float64 {
	if isCJKRune(r) {
		return tokenWeightCJK
	}
	return tokenWeightASCII
}

func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r)
}
