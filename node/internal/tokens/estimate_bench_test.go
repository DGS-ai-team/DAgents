package tokens

import (
	"strings"
	"testing"
)

func BenchmarkEstimate_ASCII4K(b *testing.B) {
	text := strings.Repeat("a", 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Estimate(text)
	}
}

func BenchmarkEstimate_ASCII50K(b *testing.B) {
	text := strings.Repeat("a", 50000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Estimate(text)
	}
}

func BenchmarkEstimate_Han5K(b *testing.B) {
	text := strings.Repeat("测", 5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Estimate(text)
	}
}
