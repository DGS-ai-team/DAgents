package toolresult

import (
	"strings"
	"testing"
)

func benchTextASCII(n int) string {
	return strings.Repeat("o", n)
}

func benchTextHan(n int) string {
	return strings.Repeat("测", n)
}

func BenchmarkEstimateTokens_ASCII4K(b *testing.B) {
	text := benchTextASCII(4000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text)
	}
}

func BenchmarkEstimateTokens_ASCII50K(b *testing.B) {
	text := benchTextASCII(50000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text)
	}
}

func BenchmarkEstimateTokens_Han5K(b *testing.B) {
	text := benchTextHan(5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text)
	}
}

func BenchmarkClipToTokenBudget_ASCII50K_3000(b *testing.B) {
	text := benchTextASCII(50000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClipToTokenBudget(text, 3000)
	}
}

func BenchmarkClipToTokenBudget_Han5K_3000(b *testing.B) {
	text := benchTextHan(5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClipToTokenBudget(text, 3000)
	}
}

func BenchmarkClipToTokenBudget_noTruncate_ASCII4K(b *testing.B) {
	text := benchTextASCII(4000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClipToTokenBudget(text, 3000)
	}
}
