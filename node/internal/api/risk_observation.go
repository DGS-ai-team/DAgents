package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type riskObservationDTO struct {
	AgentID        string    `json:"agent_id"`
	RequestID      string    `json:"request_id"`
	ToolName       string    `json:"tool_name"`
	ArgsDigest     string    `json:"args_digest"`
	PolicyAction   string    `json:"policy_action"`
	Level          string    `json:"level"`
	Reason         string    `json:"reason"`
	Recommendation string    `json:"recommendation"`
	RiskUnknown    bool      `json:"risk_unknown"`
	UsageUnknown   bool      `json:"usage_unknown"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Server) attachRiskObserver(opts *session.TurnOptions, client llm.Client, rec store.AgentRecord, snap agentruntime.Snapshot) {
	if s == nil || opts == nil || client == nil || s.goalStore == nil || !opts.RiskObservationEnabled || !strings.EqualFold(snap.AgentType, "auto") {
		return
	}
	const maxArgsBytes = 32 << 10
	opts.RiskSubmitter = hooks.NewRiskDispatcher(hooks.RiskDispatcherConfig{
		Host: turn.RiskLLMHost{Client: client}, Store: hooks.GoalsRiskReviewStore{Store: s.goalStore}, AgentID: rec.AgentID,
		Capacity: 32, Timeout: 2 * time.Second, EstimatedTokens: 256, MaxArgsBytes: maxArgsBytes,
	})
}

func (s *Server) handleListRiskObservations(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if s == nil || s.agents == nil || s.goalStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "risk_unavailable", "risk observations unavailable", nil)
		return
	}
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_lookup_failed", err.Error(), nil)
		return
	}
	if rec == nil || rec.Archived {
		s.writeAgentNotFound(w, id)
		return
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil || !strings.EqualFold(snap.AgentType, "auto") {
		writeAPIError(w, http.StatusForbidden, "risk_agent_required", "risk observations require an Auto Agent", nil)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, parseErr := strconv.Atoi(raw)
		if parseErr != nil || n <= 0 || n > 1000 {
			writeAPIError(w, http.StatusBadRequest, "invalid_limit", fmt.Sprintf("limit must be between 1 and 1000: %q", raw), nil)
			return
		}
		limit = n
	}
	stored := s.goalStore.ListRiskObservations(id, limit)
	observations := make([]riskObservationDTO, 0, len(stored))
	for _, item := range stored {
		observations = append(observations, riskObservationDTO{
			AgentID: item.AgentID, RequestID: item.RequestID, ToolName: item.ToolName,
			ArgsDigest: item.ArgsDigest, PolicyAction: item.PolicyAction, Level: item.Level,
			Reason: item.Reason, Recommendation: item.Recommendation, RiskUnknown: item.RiskUnknown,
			UsageUnknown: item.UsageUnknown, CreatedAt: item.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": id, "observations": observations})
}
