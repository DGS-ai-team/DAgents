package triggers

import (
	"strings"

	clihitl "github.com/DGS-ai-team/DAgents/node/internal/hitl"
)

// EffectiveSessionTargetMode 缺省为 fixed（兼容旧 triggers.json）。
func (d Definition) EffectiveSessionTargetMode() SessionTargetMode {
	if strings.TrimSpace(string(d.SessionTargetMode)) != "" {
		return d.SessionTargetMode
	}
	return SessionTargetFixed
}

func hasBoundSessionID(d Definition) bool {
	return d.TargetSessionID != nil && strings.TrimSpace(*d.TargetSessionID) != ""
}

// SessionConfigFromApprovalTarget 将审批选项映射为持久化/一次性 fire 配置。
func SessionConfigFromApprovalTarget(approvalTarget, currentSessionID string) (SessionTargetMode, *string) {
	switch strings.TrimSpace(approvalTarget) {
	case clihitl.TriggerSessionNew:
		return SessionTargetNewSession, nil
	case clihitl.TriggerSessionLatestActive:
		return SessionTargetLatestActive, nil
	default:
		id := strings.TrimSpace(currentSessionID)
		if id == "" {
			return SessionTargetFixed, nil
		}
		return SessionTargetFixed, &id
	}
}

// FireOptions 手动 fire 时的一次性会话 override（不改 trigger 定义，bind 除外）。
type FireOptions struct {
	SessionTargetMode SessionTargetMode
	FixedSessionID    string
}

// FireOptionsFromApprovalTarget 构造 trigger_fire 审批通过后的 fire override。
func FireOptionsFromApprovalTarget(approvalTarget, currentSessionID string, def Definition) *FireOptions {
	target := strings.TrimSpace(approvalTarget)
	if target == "" {
		target = clihitl.TriggerSessionSame
	}
	if target == clihitl.TriggerSessionNew &&
		def.EffectiveSessionTargetMode() == SessionTargetFixed &&
		hasBoundSessionID(def) {
		return nil
	}
	mode, sessionPtr := SessionConfigFromApprovalTarget(target, currentSessionID)
	opts := &FireOptions{SessionTargetMode: mode}
	if sessionPtr != nil {
		opts.FixedSessionID = *sessionPtr
	}
	return opts
}
