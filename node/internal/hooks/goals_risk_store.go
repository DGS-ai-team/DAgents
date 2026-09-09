package hooks

import (
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
)

// GoalsRiskReviewStore adapts the durable Goals receipts to the hook worker
// contract. Agent identity remains supplied by the dispatcher, never by the
// model or observation payload.
type GoalsRiskReviewStore struct{ Store *goals.Store }

func (a GoalsRiskReviewStore) BeginRiskReview(agentID, operationID, requestID, fingerprint string, estimated int64, now time.Time) (RiskReviewReservation, error) {
	if a.Store == nil {
		return RiskReviewReservation{}, goals.ErrNotFound
	}
	r, err := a.Store.BeginRiskReview(agentID, operationID, requestID, fingerprint, estimated, now)
	return RiskReviewReservation{Claimed: r.Claimed}, err
}

func (a GoalsRiskReviewStore) SettleRiskReviewWithObservation(agentID, operationID string, used int64, unknown bool, in RiskObservationRecord, now time.Time) error {
	if a.Store == nil {
		return goals.ErrNotFound
	}
	_, err := a.Store.SettleRiskReviewWithObservation(agentID, operationID, used, unknown, goals.RiskObservationRecord{
		AgentID: strings.TrimSpace(in.AgentID), OperationID: strings.TrimSpace(in.OperationID), RequestID: strings.TrimSpace(in.RequestID),
		ToolName: in.ToolName, ArgsDigest: in.ArgsDigest, PolicyAction: in.PolicyAction, Level: in.Level,
		Reason: in.Reason, Recommendation: in.Recommendation, RiskUnknown: in.RiskUnknown, UsageUnknown: in.UsageUnknown, Error: in.Error,
		CreatedAt: now.UTC(),
	}, now)
	return err
}

var _ RiskReviewStore = GoalsRiskReviewStore{}
