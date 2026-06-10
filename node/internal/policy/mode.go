package policy

import "strings"

// ApprovalMode 对齐 Python 工具/shell 策略：always / never / rule。
type ApprovalMode string

const (
	ModeAlways ApprovalMode = "always"
	ModeNever  ApprovalMode = "never"
	ModeRule   ApprovalMode = "rule"
	ModeDeny   ApprovalMode = "deny"
)

func normalizeMode(raw string, fallback ApprovalMode) ApprovalMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ModeAlways):
		return ModeAlways
	case string(ModeNever):
		return ModeNever
	case string(ModeRule):
		return ModeRule
	case string(ModeDeny):
		return ModeDeny
	default:
		return fallback
	}
}
