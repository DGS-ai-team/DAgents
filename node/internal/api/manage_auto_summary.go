package api

import (
	"context"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/manage"
)

// autoSummaryProvider returns only the bounded projection intended for Manage.
// In particular, role objectives, checkpoints, prompts, workspaces, and paths
// never cross the Node/Manage boundary.
func (s *Server) autoSummaryProvider() manage.AutoSummaryProvider {
	return func(ctx context.Context) ([]manage.AutoEmployeeSummary, error) {
		if s.agents == nil || s.autonomyStore == nil {
			return nil, nil
		}
		records, err := s.agents.List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]manage.AutoEmployeeSummary, 0, len(records))
		for _, rec := range records {
			if rec.Archived {
				continue
			}
			snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
			if err != nil || !strings.EqualFold(strings.TrimSpace(snap.AgentType), "auto") {
				continue
			}
			profile, configured := s.autonomyStore.GetProfile(rec.AgentID)
			state, reason, next := s.projectAutoActivation(rec.AgentID, profile, configured, time.Now().UTC())
			dreaming := s.projectAutoDreaming(rec.AgentID, time.Now().UTC())
			var dreamingNext, dreamingSuccess *time.Time
			if dreaming.NextAt != nil {
				v := *dreaming.NextAt
				dreamingNext = &v
			}
			if dreaming.LastSuccess != nil {
				v := *dreaming.LastSuccess
				dreamingSuccess = &v
			}
			name := []rune(rec.DisplayName)
			if len(name) > 256 {
				name = name[:256]
			}
			counts := map[string]int{}
			for _, todo := range s.autonomyStore.ListTodos(rec.AgentID) {
				status := strings.TrimSpace(todo.Status)
				if status == "" {
					status = "unknown"
				}
				counts[status]++
			}
			out = append(out, manage.AutoEmployeeSummary{AgentID: rec.AgentID, DisplayName: string(name), Role: "Auto employee", WakeIntervalSeconds: profile.WakeIntervalSeconds, TodoCounts: counts, State: state, Reason: reason, NextAt: next, Dreaming: manage.DreamingSummary{State: dreaming.State, NextAt: dreamingNext, LastSuccess: dreamingSuccess}, ProfileRevision: profile.Revision, RuntimeRevision: rec.RuntimeRevision, AsOf: time.Now().UTC()})
		}
		return out, nil
	}
}
