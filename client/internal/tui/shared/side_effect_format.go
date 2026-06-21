package shared

import (
	"fmt"
	"strings"
)

// FormatSideEffectSeqLine 格式化 side_effect_applied / side_effects_cleared 系统行。
func FormatSideEffectSeqLine(label string, raw any) string {
	seqs, ok := raw.([]any)
	if !ok || len(seqs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(seqs))
	for _, s := range seqs {
		parts = append(parts, fmt.Sprint(s))
	}
	return fmt.Sprintf("旁路回调 %s: #%s", label, strings.Join(parts, ", #"))
}
