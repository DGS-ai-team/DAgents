package policy

import "strings"

// Decision 为 API/Client 可见的三档策略。
type Decision string

const (
	DecisionAllowAuto       Decision = "allow_auto"
	DecisionRequireApproval Decision = "require_approval"
	DecisionDeny            Decision = "deny"
)

// ModeToDecision 将内部 mode 映射为 API decision。
func ModeToDecision(mode ApprovalMode) Decision {
	switch mode {
	case ModeNever:
		return DecisionAllowAuto
	case ModeDeny:
		return DecisionDeny
	default:
		return DecisionRequireApproval
	}
}

// DecisionToMode 将 API decision 映射为 txt 写入 mode。
func DecisionToMode(d Decision) (ApprovalMode, error) {
	switch Decision(strings.TrimSpace(string(d))) {
	case DecisionAllowAuto:
		return ModeNever, nil
	case DecisionRequireApproval:
		return ModeAlways, nil
	case DecisionDeny:
		return ModeDeny, nil
	default:
		return "", fmtInvalidDecision(d)
	}
}

type invalidDecisionError struct{ d Decision }

func fmtInvalidDecision(d Decision) error { return invalidDecisionError{d: d} }

func (e invalidDecisionError) Error() string {
	return "invalid decision: " + string(e.d)
}

// ParseDecision 解析 API/Client 传入的 decision 字符串。
func ParseDecision(raw string) (Decision, error) {
	d := Decision(strings.TrimSpace(raw))
	switch d {
	case DecisionAllowAuto, DecisionRequireApproval, DecisionDeny:
		return d, nil
	default:
		return "", fmtInvalidDecision(d)
	}
}
