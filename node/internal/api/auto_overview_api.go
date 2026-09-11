package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

type autoOverviewItem struct {
	AgentID             string                       `json:"agent_id"`
	DisplayName         string                       `json:"display_name"`
	AgentType           string                       `json:"agent_type"`
	Workspace           agentruntime.WorkspaceConfig `json:"workspace"`
	Responsibility      string                       `json:"responsibility,omitempty"`
	WakeIntervalSeconds int64                        `json:"wake_interval_seconds,omitempty"`
	TodoCounts          map[string]int               `json:"todo_counts"`
	TodoSummary         []string                     `json:"todo_summary,omitempty"`
	State               string                       `json:"state"`
	StateReason         string                       `json:"state_reason,omitempty"`
	NextAt              *time.Time                   `json:"next_at,omitempty"`
	ProfileRevision     int64                        `json:"profile_revision,omitempty"`
	RuntimeRevision     int64                        `json:"runtime_revision,omitempty"`
	Dreaming            dreamingOverview             `json:"dreaming"`
}

type dreamingOverview struct {
	State       string     `json:"state"`
	NextAt      *time.Time `json:"next_at,omitempty"`
	LastSuccess *time.Time `json:"last_success,omitempty"`
}

func (s *Server) projectAutoDreaming(agentID string, now time.Time) dreamingOverview {
	if s.dreamingSched == nil {
		return dreamingOverview{State: "unknown"}
	}
	status := s.dreamingSched.CurrentStatus(agentID, now)
	view := dreamingOverview{State: status.State}
	if !status.NextAt.IsZero() {
		next := status.NextAt
		view.NextAt = &next
	}
	if !status.LastSuccess.IsZero() {
		success := status.LastSuccess
		view.LastSuccess = &success
	}
	return view
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

func (s *Server) projectAutoActivation(agentID string, profile autonomy.Profile, configured bool, now time.Time) (string, string, *time.Time) {
	state, reason := "activation_off", ""
	var next *time.Time
	if configured && profile.WakeIntervalSeconds > 0 {
		if s.triggerStore == nil {
			state, reason = "needs_attention", "trigger store unavailable"
		} else if d, ok := s.triggerStore.GetTrigger(triggers.AutoDefaultTriggerID(agentID)); !ok {
			state, reason = "needs_attention", "default trigger missing"
		} else if d.RecoveryRequired {
			state, reason = "needs_attention", "default trigger recovery required"
		} else if interval, valid := triggers.ConfiguredIntervalSeconds(d.Condition); !valid || interval != profile.WakeIntervalSeconds {
			state, reason = "needs_attention", "default trigger interval mismatch"
		} else if !d.Enabled {
			state, reason = "needs_attention", "default trigger disabled"
		} else {
			state = "standby"
			if d.NextFireAt != nil {
				at := time.Unix(int64(*d.NextFireAt), 0).UTC()
				if at.After(now) {
					next = &at
				}
			}
		}
	}
	if s.sessions != nil {
		_, active, runtimeState, _ := s.sessions.RuntimeInfo(agentID)
		if active {
			state, reason = "working", string(runtimeState)
		}
	}
	return state, reason, next
}

func (s *Server) handleAutoOverview(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil || s.autonomyStore == nil || s.triggerStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "overview_unavailable", "auto overview stores unavailable", nil)
		return
	}
	agents, err := s.agents.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "overview_unavailable", err.Error(), nil)
		return
	}
	items := make([]autoOverviewItem, 0, len(agents))
	for _, rec := range agents {
		snap, parseErr := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
		if rec.Archived || parseErr != nil || strings.ToLower(strings.TrimSpace(snap.AgentType)) != "auto" {
			continue
		}
		item := autoOverviewItem{AgentID: rec.AgentID, DisplayName: rec.DisplayName, AgentType: "auto", Workspace: snap.Workspace, State: "activation_off", TodoCounts: map[string]int{}, RuntimeRevision: rec.RuntimeRevision}
		profile, configured := s.autonomyStore.GetProfile(rec.AgentID)
		for _, todo := range s.autonomyStore.ListTodos(rec.AgentID) {
			status := strings.TrimSpace(todo.Status)
			if status == "" {
				status = "unknown"
			}
			item.TodoCounts[status]++
			if len(item.TodoSummary) < 3 && strings.TrimSpace(todo.Text) != "" {
				item.TodoSummary = append(item.TodoSummary, todo.Text)
			}
		}
		if configured {
			item.ProfileRevision, item.Responsibility, item.WakeIntervalSeconds = profile.Revision, profile.Responsibility, profile.WakeIntervalSeconds
		}
		item.State, item.StateReason, item.NextAt = s.projectAutoActivation(rec.AgentID, profile, configured, time.Now().UTC())
		item.Dreaming = s.projectAutoDreaming(rec.AgentID, time.Now().UTC())
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		priority := map[string]int{"needs_attention": 0, "working": 1, "standby": 2, "activation_off": 3}
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
		if search != "" && !strings.Contains(strings.ToLower(item.AgentID+" "+item.DisplayName+" "+item.Responsibility), search) {
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
	counts := map[string]int{"needs_attention": 0, "working": 0, "standby": 0, "activation_off": 0}
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
