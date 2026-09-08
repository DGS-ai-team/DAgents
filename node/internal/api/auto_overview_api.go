package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

type autoOverviewItem struct {
	AgentID         string                       `json:"agent_id"`
	DisplayName     string                       `json:"display_name"`
	AgentType       string                       `json:"agent_type"`
	Workspace       agentruntime.WorkspaceConfig `json:"workspace"`
	State           string                       `json:"state"`
	StateReason     string                       `json:"state_reason,omitempty"`
	RoleObjective   string                       `json:"role_objective,omitempty"`
	GoalID          string                       `json:"goal_id,omitempty"`
	GoalTitle       string                       `json:"goal_title,omitempty"`
	GoalStatus      string                       `json:"goal_status,omitempty"`
	LastSummary     string                       `json:"last_summary,omitempty"`
	TokensUsed      int64                        `json:"tokens_used,omitempty"`
	TokenBudget     int64                        `json:"token_budget,omitempty"`
	UnknownUsage    bool                         `json:"unknown_usage,omitempty"`
	NextAt          *time.Time                   `json:"next_at,omitempty"`
	ProfileRevision int64                        `json:"profile_revision,omitempty"`
	RuntimeRevision int64                        `json:"runtime_revision,omitempty"`
	Summary         map[string]any               `json:"summary,omitempty"`
}

type autoOverviewResponse struct {
	Items    []autoOverviewItem `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Counts   map[string]int     `json:"counts"`
	AsOf     time.Time          `json:"as_of"`
	Revision string             `json:"revision"`
}

func (s *Server) projectAutoSummary(agentID string, p goals.AutoProfile, g *goals.Goal) map[string]any {
	state, reason := "disabled", ""
	var next *time.Time
	last := ""
	usage, _ := s.goalStore.GetUsage(agentID)
	if p.AgentID == "" {
		state = "unconfigured"
	} else if p.Enabled {
		state = "standby"
	}
	if g != nil && p.Enabled {
		state, reason = autoGoalState(*g, s.goalStore.Runs(g.ID))
		if p.PlanMode == "recurring" && g.Status == goals.StatusCompleted && p.CurrentGoalID == g.ID {
			if r := s.recurringConfigurationReason(context.Background(), agentID, p); r != "" {
				state, reason = "standby", r
			}
		}
	}
	if g != nil {
		if g.LastCheckpoint != nil {
			if g.LastCheckpoint.Decision != nil && strings.TrimSpace(g.LastCheckpoint.Decision.Summary) != "" {
				last = g.LastCheckpoint.Decision.Summary
			} else {
				last = g.LastCheckpoint.Summary
			}
		}
		if s.triggerStore != nil {
			if d, ok := s.triggerStore.GetTrigger(g.TriggerID); ok {
				next = autoTriggerNextAt(*d, *g, p.Enabled, agentID, time.Now().UTC())
			}
		}
	}
	return map[string]any{"state": state, "state_reason": reason, "next_at": next, "role": p.RoleObjective, "last_summary": last, "usage": usage}
}

func (s *Server) handleAutoOverview(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil || s.goalStore == nil || s.triggerStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "overview_unavailable", "auto overview stores unavailable", nil)
		return
	}
	agents, err := s.agents.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "overview_unavailable", err.Error(), nil)
		return
	}
	goalsByAgent := make(map[string][]goals.Goal)
	for _, g := range s.goalStore.List() {
		if g.Managed {
			goalsByAgent[strings.TrimSpace(g.AgentID)] = append(goalsByAgent[strings.TrimSpace(g.AgentID)], g)
		}
	}
	triggerDefs := s.triggerStore.ListTriggers()
	triggerByID := make(map[string]triggers.Definition)
	for _, d := range triggerDefs {
		triggerByID[d.TriggerID] = d
	}

	items := make([]autoOverviewItem, 0, len(agents))
	for _, rec := range agents {
		snap, parseErr := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
		if rec.Archived || parseErr != nil || strings.ToLower(strings.TrimSpace(snap.AgentType)) != "auto" {
			continue
		}
		item := autoOverviewItem{AgentID: rec.AgentID, DisplayName: rec.DisplayName, AgentType: "auto", Workspace: snap.Workspace, State: "unconfigured", RuntimeRevision: rec.RuntimeRevision}
		profile, configured := s.goalStore.GetProfile(rec.AgentID)
		if configured {
			item.ProfileRevision = profile.Revision
			item.RoleObjective = profile.RoleObjective
			if profile.Enabled {
				item.State = "standby"
			} else {
				item.State = "disabled"
			}
		}
		if !configured {
			item.Summary = s.projectAutoSummary(rec.AgentID, goals.AutoProfile{}, nil)
		}
		usage, _ := s.goalStore.GetUsage(rec.AgentID)
		item.TokensUsed = usage.BusinessTokens + usage.MaintenanceTokens
		item.UnknownUsage = usage.Unknown
		if configured && profile.CurrentGoalID != "" {
			for _, g := range goalsByAgent[rec.AgentID] {
				if g.ID != profile.CurrentGoalID {
					continue
				}
				item.GoalID, item.GoalTitle = g.ID, g.Title
				item.GoalStatus, item.TokenBudget = string(g.Status), g.TokenBudget
				if g.LastCheckpoint != nil {
					item.LastSummary = g.LastCheckpoint.Summary
				}
				if profile.Enabled {
					item.State, item.StateReason = autoGoalState(g, s.goalStore.Runs(g.ID))
				}
				if d, ok := triggerByID[g.TriggerID]; ok {
					item.NextAt = autoTriggerNextAt(d, g, profile.Enabled, rec.AgentID, time.Now().UTC())
				}
				break
			}
		}
		if configured {
			summary := s.projectAutoSummary(rec.AgentID, profile, func() *goals.Goal {
				if profile.CurrentGoalID == "" {
					return nil
				}
				g, ok := s.goalStore.Get(profile.CurrentGoalID)
				if !ok {
					return nil
				}
				return &g
			}())
			item.Summary = summary
			if summary["state"] != item.State {
				item.State = summary["state"].(string)
			}
			if v, ok := summary["state_reason"].(string); ok {
				item.StateReason = v
			}
			item.LastSummary = summary["last_summary"].(string)
			item.NextAt = summary["next_at"].(*time.Time)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		priority := map[string]int{"needs_attention": 0, "running": 1, "paused": 2, "standby": 3, "disabled": 4, "unconfigured": 5, "completed": 6, "stopped": 7, "failed": 8}
		if priority[items[i].State] != priority[items[j].State] {
			return priority[items[i].State] < priority[items[j].State]
		}
		if items[i].DisplayName != items[j].DisplayName {
			return items[i].DisplayName < items[j].DisplayName
		}
		return items[i].AgentID < items[j].AgentID
	})
	q := r.URL.Query()
	search, status, workspace := strings.ToLower(strings.TrimSpace(q.Get("search"))), strings.ToLower(strings.TrimSpace(q.Get("status"))), strings.TrimSpace(q.Get("workspace"))
	matched := items[:0]
	for _, item := range items {
		if search != "" && !strings.Contains(strings.ToLower(item.AgentID+" "+item.DisplayName+" "+item.RoleObjective+" "+item.GoalTitle+" "+item.LastSummary), search) {
			continue
		}
		if workspace != "" && item.Workspace.Path != workspace && item.Workspace.Mode != workspace {
			continue
		}
		matched = append(matched, item)
	}
	filtered := matched
	if status != "" {
		filtered = make([]autoOverviewItem, 0, len(matched))
		for _, item := range matched {
			if item.State == status {
				filtered = append(filtered, item)
			}
		}
	}
	page, pageSize := queryPositive(q.Get("page"), 1), queryPositive(q.Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	total := len(filtered)
	if total > 0 && (page-1)*pageSize >= total {
		page = (total-1)/pageSize + 1
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	counts := map[string]int{"unconfigured": 0, "disabled": 0, "paused": 0, "running": 0, "needs_attention": 0, "standby": 0, "completed": 0, "stopped": 0, "failed": 0}
	counts["total"] = len(matched)
	for _, item := range matched {
		counts[item.State]++
	}
	asOf := time.Now().UTC()
	seed, _ := json.Marshal(struct {
		A []autoOverviewItem
		T time.Time
	}{filtered, asOf})
	sum := sha256.Sum256(seed)
	writeJSON(w, http.StatusOK, autoOverviewResponse{Items: filtered[start:end], Total: total, Page: page, PageSize: pageSize, Counts: counts, AsOf: asOf, Revision: "snapshot-" + hex.EncodeToString(sum[:8])})
}

func autoTriggerNextAt(d triggers.Definition, g goals.Goal, profileEnabled bool, agentID string, now time.Time) *time.Time {
	if !profileEnabled || !d.Enabled || d.NextFireAt == nil || d.ManagedGoalID != g.ID || d.OwnerAgentID != agentID || d.TargetAgentID != agentID || d.Controller != "goal" || d.ControllerID != g.ID || (g.Status != goals.StatusActive && g.Status != goals.StatusWaiting) {
		return nil
	}
	at := time.Unix(int64(*d.NextFireAt), int64((*d.NextFireAt-float64(int64(*d.NextFireAt)))*1e9)).UTC()
	if !at.After(now) {
		return nil
	}
	return &at
}

func autoGoalState(g goals.Goal, runs []goals.Run) (string, string) {
	if g.Status == goals.StatusFailed {
		return "needs_attention", g.StatusReason
	}
	if g.Status == goals.StatusCompleted || g.Status == goals.StatusStopped {
		return "standby", g.StatusReason
	}
	for _, run := range runs {
		if run.FinishedAt == nil {
			if g.Status == goals.StatusWaiting && g.StatusReason == "approval_required" {
				return "needs_attention", g.StatusReason
			}
			return "running", run.Reason
		}
	}
	if g.Status == goals.StatusWaiting && g.StatusReason == "approval_required" {
		return "needs_attention", g.StatusReason
	}
	if g.Status == goals.StatusPaused {
		return "paused", g.StatusReason
	}
	return "standby", g.StatusReason
}

func queryPositive(raw string, fallback int) int {
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return fallback
		}
		n = n*10 + int(c-'0')
		if n > 1000000 {
			return fallback
		}
	}
	if n < 1 {
		return fallback
	}
	return n
}
