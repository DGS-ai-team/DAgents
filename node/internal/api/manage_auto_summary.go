package api

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/manage"
)

// autoSummaryProvider returns only the bounded projection intended for Manage.
// In particular, role objectives, checkpoints, prompts, workspaces, and paths
// never cross the Node/Manage boundary.
func (s *Server) autoSummaryProvider() manage.AutoSummaryProvider {
	return func(ctx context.Context) ([]manage.AutoEmployeeSummary, error) {
		if s.agents == nil || s.goalStore == nil {
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
			profile, configured := s.goalStore.GetProfile(rec.AgentID)
			var goal *goals.Goal
			if configured && profile.CurrentGoalID != "" {
				if candidate, ok := s.goalStore.Get(profile.CurrentGoalID); ok && candidate.Managed && candidate.AgentID == rec.AgentID {
					goal = &candidate
				}
			}
			state, reason := "unconfigured", ""
			var next *time.Time
			lastResult := ""
			if configured {
				state = "disabled"
				if profile.Enabled {
					state = "standby"
				}
				projected := s.projectAutoSummary(rec.AgentID, profile, goal)
				if profile.Enabled {
					if v, ok := projected["state"].(string); ok {
						state = v
					}
					reason = safeAutoReason(state)
				}
				if goal != nil {
					lastResult = string(goal.Status)
				}
				if profile.Enabled {
					if v, ok := projected["next_at"].(*time.Time); ok {
					next = v
					}
				}
			}
			usage, _ := s.goalStore.GetUsage(rec.AgentID)
			tokens := usage.BusinessTokens
			if usage.MaintenanceTokens > 0 && tokens > math.MaxInt64-usage.MaintenanceTokens {
				tokens = math.MaxInt64
			} else {
				tokens += usage.MaintenanceTokens
			}
			name := []rune(rec.DisplayName)
			if len(name) > 256 {
				name = name[:256]
			}
			out = append(out, manage.AutoEmployeeSummary{
				AgentID: rec.AgentID, DisplayName: string(name), Role: "Auto employee",
				State: state, Reason: reason, LastResult: lastResult, NextAt: next,
				Usage: map[string]any{"tokens": tokens, "business_tokens": usage.BusinessTokens, "maintenance_tokens": usage.MaintenanceTokens, "unknown": usage.Unknown},
				AsOf:  time.Now().UTC(),
			})
		}
		return out, nil
	}
}

func safeAutoReason(state string) string {
	switch state {
	case "needs_attention", "running", "paused", "completed", "stopped", "failed", "waiting":
		return state
	default:
		return ""
	}
}
