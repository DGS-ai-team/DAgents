// Package tokens 提供 DeepSeek 文档一致的文本 token 粗算，供 compression、skills、toolresult 等共用。
package tokens

import (
	"math"
	"strings"
	"unicode"
)

// 与 DeepSeek 离线估算一致：https://api-docs.deepseek.com/zh-cn/quick_start/token_usage
// 1 个中文字符 ≈ 0.6 token；1 个英文字符 ≈ 0.3 token。
const (
	WeightASCII = 0.3
	WeightCJK   = 0.6
)

// Estimate 按 DeepSeek 文档粗算文本 token 数（非精确分词，以 usage 为准）。
func Estimate(text string) float64 {
	var sum float64
	for _, r := range text {
		sum += Weight(r)
	}
	return sum
}

// EstimateInt 返回 Estimate 四舍五入后的整型 token 数。
func EstimateInt(text string) int {
	return int(math.Round(Estimate(text)))
}

// Weight 返回 rune 的 token 权重（汉字 0.6，其余 0.3）。
func Weight(r rune) float64 {
	if unicode.Is(unicode.Han, r) {
		return WeightCJK
	}
	return WeightASCII
}

// TakePrefixForTokenBudget 保留 s 的前缀，使 Estimate 不超过 maxTokens。
func TakePrefixForTokenBudget(s string, maxTokens float64) string {
	if maxTokens <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0.0
	for _, r := range s {
		w := Weight(r)
		if used+w > maxTokens && used > 0 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
}

// TakeSuffixForTokenBudget 保留 s 的后缀，使 Estimate 不超过 maxTokens。
func TakeSuffixForTokenBudget(s string, maxTokens float64) string {
	if maxTokens <= 0 {
		return ""
	}
	runes := []rune(s)
	var picked []rune
	used := 0.0
	for i := len(runes) - 1; i >= 0; i-- {
		w := Weight(runes[i])
		if used+w > maxTokens && used > 0 {
			break
		}
		picked = append([]rune{runes[i]}, picked...)
		used += w
	}
	return string(picked)
}

// ClipToTokenBudget 将文本截到 token 预算内（保留前缀）；第二返回值表示是否发生截断。
func ClipToTokenBudget(text string, maxTokens float64) (string, bool) {
	if maxTokens <= 0 || Estimate(text) <= maxTokens {
		return text, false
	}
	return TakePrefixForTokenBudget(text, maxTokens), true
}
