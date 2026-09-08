package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
)

// bindRecurringAuthorization validates and binds the current user-managed
// policy snapshot. It is called only from the authenticated HTTP profile save
// path; model turns have no path to this binding operation.
func (s *Server) bindRecurringAuthorization(ctx context.Context, agentID string, p *goals.AutoProfile) error {
	if p == nil || p.PlanMode != "recurring" {
		return nil
	}
	if p.CycleDurationSeconds <= 0 || p.CycleDurationSeconds > 31*24*60*60 {
		return fmt.Errorf("configuration_required: recurring cycle duration is required")
	}
	if _, err := goals.ParseWorkSchedule(p.WorkSchedule); err != nil {
		return fmt.Errorf("configuration_required: invalid work schedule")
	}
	if strings.TrimSpace(p.WorkSchedule) == "" || strings.TrimSpace(p.Timezone) == "" {
		return fmt.Errorf("configuration_required: schedule and timezone are required")
	}
	if _, err := goals.LoadScheduleLocation(p.Timezone); err != nil {
		return fmt.Errorf("configuration_required: invalid timezone")
	}
	if s.agents == nil {
		return fmt.Errorf("agents unavailable")
	}
	rec, err := s.agents.Get(ctx, agentID)
	if err != nil || rec == nil || rec.Archived {
		return fmt.Errorf("agent not found")
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil || snap.AgentType != "auto" {
		return fmt.Errorf("agent is not auto")
	}
	pol, err := s.agents.EnsureAgentPolicy(ctx, agentID)
	if err != nil {
		return err
	}
	b, err := json.Marshal(struct {
		AgentID string                       `json:"agent_id"`
		Runtime int64                        `json:"runtime_revision"`
		Tools   map[string]string            `json:"tools"`
		Shell   map[string]map[string]string `json:"shell"`
	}{agentID, rec.RuntimeRevision, pol.Tools, pol.Shell})
	if err != nil {
		return err
	}
	h := sha256.Sum256(b)
	p.AuthorizationRef = hex.EncodeToString(h[:])
	p.AuthorizationRevision = rec.RuntimeRevision
	return nil
}

func (s *Server) recurringAuthorizationCurrent(ctx context.Context, agentID string, p goals.AutoProfile) bool {
	q := p
	if err := s.bindRecurringAuthorization(ctx, agentID, &q); err != nil {
		return false
	}
	return q.AuthorizationRef == p.AuthorizationRef && q.AuthorizationRevision == p.AuthorizationRevision
}

func (s *Server) recurringConfigurationReason(ctx context.Context, agentID string, p goals.AutoProfile) string {
	if p.PlanMode != "recurring" { return "" }
	if p.CycleDurationSeconds <= 0 || p.CycleDurationSeconds > 31*24*60*60 || strings.TrimSpace(p.WorkSchedule) == "" || strings.TrimSpace(p.Timezone) == "" { return "configuration_required" }
	if _, err := goals.ParseWorkSchedule(p.WorkSchedule); err != nil { return "configuration_required" }
	if _, err := goals.LoadScheduleLocation(p.Timezone); err != nil { return "configuration_required" }
	if !s.recurringAuthorizationCurrent(ctx, agentID, p) { return "authorization_changed" }
	return ""
}

// reconcileAutoCycles creates at most one child cycle per completed recurring
// Goal. It is deliberately before projection: the Store commit creates the
// pending intent and the existing projector remains the only trigger writer.
func (s *Server) reconcileAutoCycles(ctx context.Context, now time.Time) error {
	if s == nil || s.goalStore == nil || s.agents == nil {
		return nil
	}
	for _, old := range s.goalStore.List() {
		if !old.Managed || old.Status != goals.StatusCompleted {
			continue
		}
		p, ok := s.goalStore.GetProfile(old.AgentID)
		if !ok || !p.Enabled || p.PlanMode != "recurring" || p.CurrentGoalID != old.ID {
			continue
		}
		if !s.recurringAuthorizationCurrent(ctx, old.AgentID, p) {
			continue
		}
		loc, err := goals.LoadScheduleLocation(p.Timezone)
		if err != nil {
			continue
		}
		sch, err := goals.ParseWorkSchedule(p.WorkSchedule)
		if err != nil || sch == nil || p.CycleDurationSeconds <= 0 {
			continue
		}
		runs := s.goalStore.Runs(old.ID)
		var completed goals.Run
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i].Status == "completed" && runs[i].FinishedAt != nil && runs[i].TokensUsed > 0 {
				completed = runs[i]
				break
			}
		}
		if completed.ID == "" {
			continue
		}
		occurrence, ok := sch.NextOccurrence(completed.FinishedAt.UTC(), loc)
		if !ok {
			continue
		}
		coalesced := !occurrence.After(now.UTC())
		dispatch := occurrence
		if coalesced {
			dispatch = now.UTC().Add(time.Duration(old.MinWakeIntervalSeconds) * time.Second)
		}
		_, err = s.goalStore.CreateNextRecurringCycle(goals.RecurringCycleInput{
			AgentID: old.AgentID, PreviousGoalID: old.ID, CompletedRunID: completed.ID,
			Occurrence: occurrence, CycleDuration: time.Duration(p.CycleDurationSeconds) * time.Second,
			DispatchAt: dispatch, Coalesced: coalesced,
			SessionID: old.SessionID, AuthorizationRef: p.AuthorizationRef,
			AuthorizationRevision: p.AuthorizationRevision, ExpectedProfileRevision: p.Revision,
			IdempotencyKey: fmt.Sprintf("recurring:%s:%s:%s", old.AgentID, old.ID, occurrence.Format(time.RFC3339Nano)), Now: now.UTC(),
		})
		if err != nil {
			// Eligibility can change between the read and the atomic Store CAS;
			// leave the completed history intact and retry on a later tick.
			if errors.Is(err, goals.ErrConflict) || errors.Is(err, goals.ErrUsageUnknown) || strings.Contains(err.Error(), "not eligible") || strings.Contains(err.Error(), "budget") || strings.Contains(err.Error(), "unresolved") || strings.Contains(err.Error(), "expired") {
				continue
			}
			return err
		}
	}
	return nil
}
