package goals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RecurringCycleInput is produced by the trusted controller after it has
// evaluated the profile schedule and authorization. The occurrence is part of
// the idempotency identity; callers cannot choose an arbitrary replay key.
type RecurringCycleInput struct {
	AgentID                 string
	PreviousGoalID          string
	CompletedRunID          string
	Occurrence              time.Time
	DispatchAt              time.Time
	Coalesced               bool
	CycleDuration           time.Duration
	SessionID               string
	AuthorizationRef        string
	AuthorizationRevision   int64
	ExpectedProfileRevision int64
	IdempotencyKey          string
	Now                     time.Time
}

// CreateNextRecurringCycle atomically closes the controller decision to make a
// new cycle: Goal, pending intent, and profile binding are one store snapshot.
func (s *Store) CreateNextRecurringCycle(in RecurringCycleInput) (Goal, error) {
	if s == nil || strings.TrimSpace(in.AgentID) == "" || strings.TrimSpace(in.PreviousGoalID) == "" || strings.TrimSpace(in.CompletedRunID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" || in.Occurrence.IsZero() || in.CycleDuration <= 0 || in.Now.IsZero() || in.ExpectedProfileRevision <= 0 || in.AuthorizationRevision <= 0 {
		return Goal{}, fmt.Errorf("invalid recurring cycle input")
	}
	now := in.Now.UTC()
	occurrence := in.Occurrence.UTC()
	dispatch := in.DispatchAt.UTC()
	if in.DispatchAt.IsZero() {
		dispatch = occurrence
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data.Profiles[in.AgentID]
	if !ok {
		return Goal{}, fmt.Errorf("recurring profile is not eligible")
	}
	// Idempotency is checked before the current binding fence. A successful
	// request must remain replayable even after the profile points at its child.
	identity := strings.TrimSpace(in.IdempotencyKey)
	for _, existing := range s.data.Goals {
		if existing.Managed && existing.AgentID == in.AgentID && existing.IdempotencyKey == identity {
			if existing.PreviousGoalID != in.PreviousGoalID || existing.Origin != "recurring" || existing.CycleFingerprint != recurringFingerprint(in) {
				return Goal{}, ErrConflict
			}
			return cloneGoal(existing), nil
		}
	}
	if !occurrence.After(now) && !in.Coalesced {
		return Goal{}, fmt.Errorf("recurring occurrence must be in the future")
	}
	if !p.Enabled || p.PlanMode != "recurring" || p.CurrentGoalID != in.PreviousGoalID || p.Revision != in.ExpectedProfileRevision {
		return Goal{}, fmt.Errorf("recurring profile is not eligible")
	}
	if in.AuthorizationRef == "" || p.AuthorizationRef == "" || in.AuthorizationRef != p.AuthorizationRef || p.AuthorizationRevision != in.AuthorizationRevision {
		return Goal{}, fmt.Errorf("authorization revision is not current")
	}
	prev, ok := s.data.Goals[in.PreviousGoalID]
	if !ok || !prev.Managed || prev.AgentID != in.AgentID || prev.Status != StatusCompleted {
		return Goal{}, fmt.Errorf("previous cycle is not completed")
	}
	var completed Run
	found := false
	for _, r := range s.data.Runs[prev.ID] {
		if r.ID == in.CompletedRunID {
			completed, found = r, true
			break
		}
	}
	if !found || completed.FinishedAt == nil || completed.Status != "completed" || completed.TokensUsed <= 0 {
		return Goal{}, fmt.Errorf("completed run is not eligible")
	}
	if in.Coalesced && (!occurrence.After(completed.FinishedAt.UTC()) || occurrence.After(now)) {
		return Goal{}, fmt.Errorf("invalid coalesced occurrence")
	}
	minimumDispatch := now.Add(time.Duration(prev.MinWakeIntervalSeconds) * time.Second)
	if in.Coalesced {
		if dispatch.Before(minimumDispatch) {
			return Goal{}, fmt.Errorf("invalid coalesced dispatch")
		}
	} else if !dispatch.Equal(occurrence) {
		return Goal{}, fmt.Errorf("invalid recurring dispatch")
	}
	if strings.TrimSpace(in.SessionID) != "" && strings.TrimSpace(in.SessionID) != prev.SessionID {
		return Goal{}, fmt.Errorf("session does not belong to previous cycle")
	}
	for _, r := range s.data.Runs[prev.ID] {
		if r.FinishedAt == nil || r.Status == "unknown" {
			return Goal{}, fmt.Errorf("previous cycle has unresolved run")
		}
	}
	u := s.data.Usage[in.AgentID]
	if u.Unknown || u.UnknownTokens > 0 {
		return Goal{}, ErrUsageUnknown
	}
	if p.BusinessTokenBudget > 0 && u.BusinessTokens >= p.BusinessTokenBudget {
		return Goal{}, fmt.Errorf("agent business token budget exhausted")
	}
	if p.TotalTokenBudget > 0 && (u.BusinessTokens >= p.TotalTokenBudget || u.MaintenanceTokens >= p.TotalTokenBudget-u.BusinessTokens) {
		return Goal{}, fmt.Errorf("agent total token budget exhausted")
	}
	if _, err := LoadScheduleLocation(p.Timezone); err != nil {
		return Goal{}, err
	}
	schedule, err := ParseWorkSchedule(p.WorkSchedule)
	if err != nil || schedule == nil {
		return Goal{}, fmt.Errorf("invalid work_schedule")
	}
	// The controller must supply an occurrence that the profile schedule can
	// actually produce; this prevents arbitrary future timestamps being used as
	// replay keys or immediate wakes.
	loc, _ := LoadScheduleLocation(p.Timezone)
	check := occurrence.Add(-time.Minute)
	if next, valid := schedule.NextOccurrence(check, loc); !valid || !next.Equal(occurrence) {
		return Goal{}, fmt.Errorf("occurrence does not match work_schedule")
	}
	key := identity
	for _, existing := range s.data.Goals {
		if existing.Managed && existing.AgentID == in.AgentID && existing.Status != StatusCompleted && existing.Status != StatusStopped {
			return Goal{}, ErrConflict
		}
	}
	old := s.data
	old.Goals, old.Runs, old.Profiles, old.ScheduleIntents = cloneGoalsMap(s.data.Goals), cloneRunsMap(s.data.Runs), cloneProfilesMap(s.data.Profiles), cloneIntentsMap(s.data.ScheduleIntents)
	old.FinalizationFingerprints, old.Usage, old.UsageReceipts = cloneStringMap(s.data.FinalizationFingerprints), cloneUsageMap(s.data.Usage), cloneReceiptMap(s.data.UsageReceipts)
	seq := prev.CycleSequence + 1
	if seq < 1 {
		seq = 1
	}
	for _, g := range s.data.Goals {
		if g.AgentID == in.AgentID && g.CycleSequence >= seq {
			seq = g.CycleSequence + 1
		}
	}
	expires := dispatch.Add(in.CycleDuration)
	session := in.SessionID
	if session == "" {
		session = prev.SessionID
	}
	nextProfileRevision := p.Revision + 1
	fingerprint := recurringFingerprint(in)
	g := Goal{ID: uuid.NewString(), Title: prev.Title, Objective: prev.Objective, Acceptance: prev.Acceptance, AgentID: in.AgentID, Managed: true, SessionID: session, Status: StatusWaiting, MaxRuns: prev.MaxRuns, TokenBudget: prev.TokenBudget, TurnTokenBudget: prev.TurnTokenBudget, ExpiresAt: &expires, NextWakeAt: &dispatch, ScheduleOccurrence: &occurrence, DispatchAt: &dispatch, Coalesced: in.Coalesced, MinWakeIntervalSeconds: prev.MinWakeIntervalSeconds, CycleSequence: seq, ProfileRevision: nextProfileRevision, ConfigRevision: 1, Revision: 1, PreviousGoalID: prev.ID, Origin: "recurring", IdempotencyKey: key, CycleFingerprint: fingerprint, CreatedAt: now, UpdatedAt: now, EnableIntent: true}
	d := FinalDecision{Outcome: OutcomeProgress, Summary: "recurring cycle scheduled", Reason: "authorized recurring schedule", ExpectedProgress: g.Acceptance, NextAction: NextAt, NextWakeAt: &dispatch}
	intent := ScheduleIntent{ID: g.ID + ":goal", AgentID: in.AgentID, GoalID: g.ID, Purpose: "goal", Generation: int64(seq), Decision: d, DueAt: &dispatch, State: IntentPending, ProfileRevision: nextProfileRevision, UpdatedAt: now}
	intent.Fingerprint = intentFingerprint(intent)
	p.CurrentGoalID, p.Revision, p.UpdatedAt = g.ID, nextProfileRevision, now
	s.data.Goals[g.ID], s.data.Profiles[in.AgentID] = g, p
	s.data.ScheduleIntents[intentKey(g.ID, "goal")] = intent
	if err := s.saveLocked(); err != nil {
		s.data = old
		return Goal{}, err
	}
	return cloneGoal(g), nil
}

func recurringFingerprint(in RecurringCycleInput) string {
	b, _ := json.Marshal(struct {
		Agent, Previous, Run, Session, AuthRef, Key, Occurrence string
		AuthRevision, ProfileRevision                           int64
		Duration                                                int64
	}{in.AgentID, in.PreviousGoalID, in.CompletedRunID, in.SessionID, in.AuthorizationRef, in.IdempotencyKey, in.Occurrence.UTC().Format(time.RFC3339Nano), in.AuthorizationRevision, in.ExpectedProfileRevision, int64(in.CycleDuration)})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func cloneProfilesMap(m map[string]AutoProfile) map[string]AutoProfile {
	o := make(map[string]AutoProfile, len(m))
	for k, v := range m {
		o[k] = v
	}
	return o
}
