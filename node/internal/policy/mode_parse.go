package policy

import (
	"fmt"
	"strings"
)

// ParseApprovalMode 解析 API/Client 传入的四档 mode。
func ParseApprovalMode(raw string) (ApprovalMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ModeAlways):
		return ModeAlways, nil
	case string(ModeNever):
		return ModeNever, nil
	case string(ModeRule):
		return ModeRule, nil
	case string(ModeDeny):
		return ModeDeny, nil
	default:
		return "", fmt.Errorf("invalid mode: %q", raw)
	}
}
