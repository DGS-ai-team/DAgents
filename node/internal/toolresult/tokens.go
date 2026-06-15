package toolresult

import (
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

func tokenWeight(r rune) float64 {
	if isCJKRune(r) {
		return tokenWeightCJK
	}
	return tokenWeightASCII
}

func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r)
}
