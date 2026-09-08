package goals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
)

type ScheduleIntentState string

const (
	IntentPending   ScheduleIntentState = "pending"
	IntentProjected ScheduleIntentState = "projected"
	IntentRevoked   ScheduleIntentState = "revoked"
)

type ScheduleIntent struct {
	ID              string              `json:"id"`
	AgentID         string              `json:"agent_id"`
	GoalID          string              `json:"goal_id"`
	Purpose         string              `json:"purpose"`
	Generation      int64               `json:"generation"`
	Decision        FinalDecision       `json:"decision"`
	DueAt           *time.Time          `json:"due_at,omitempty"`
	EventFilter     map[string]any      `json:"event_filter,omitempty"`
	State           ScheduleIntentState `json:"state"`
	ProfileRevision int64               `json:"profile_revision"`
	Fingerprint     string              `json:"fingerprint"`
	UpdatedAt       time.Time           `json:"updated_at"`
}
type FinalizeInput struct {
	RunID                   string
	ExpectedGoalRevision    int64
	ExpectedProfileRevision int64
	Decision                FinalDecision
	ActualTokens            int64
	Purpose                 string
	Generation              int64
	RegisteredSource        map[string]bool
	Now                     time.Time
	// The lifecycle observer supplies these fields. They are intentionally
	// optional so the original explicit FinalizeRun API remains compatible.
	TerminalStatus         string
	TerminalReason         string
	ExpectedConfigRevision int64
}

func intentKey(g, p string) string { return strings.TrimSpace(g) + "\x00" + strings.TrimSpace(p) }
func cloneScheduleIntent(i ScheduleIntent) ScheduleIntent {
	o := i
	o.Decision = i.Decision.Clone()
	if i.DueAt != nil {
		t := *i.DueAt
		o.DueAt = &t
	}
	o.EventFilter = cloneDecisionMap(i.EventFilter)
	return o
}
func (s *Store) GetScheduleIntent(g, p string) (ScheduleIntent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := s.data.ScheduleIntents[intentKey(g, p)]
	if !ok {
		return ScheduleIntent{}, ErrNotFound
	}
	return cloneScheduleIntent(i), nil
}
func (s *Store) ListScheduleIntents(g string) []ScheduleIntent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o := []ScheduleIntent{}
	for _, i := range s.data.ScheduleIntents {
		if g == "" || i.GoalID == g {
			o = append(o, cloneScheduleIntent(i))
		}
	}
	return o
}
func (s *Store) UpsertScheduleIntent(i ScheduleIntent) (ScheduleIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateIntent(i); err != nil {
		return ScheduleIntent{}, err
	}
	computed := intentFingerprint(i)
	if i.Fingerprint != "" && i.Fingerprint != computed {
		return ScheduleIntent{}, ErrConflict
	}
	i.Fingerprint = computed
	k := intentKey(i.GoalID, i.Purpose)
	old, ok := s.data.ScheduleIntents[k]
	if ok && i.Generation <= old.Generation {
		if i.Generation == old.Generation && sameIntent(old, i) {
			return cloneScheduleIntent(old), nil
		}
		return ScheduleIntent{}, ErrConflict
	}
	prev := old
	s.data.ScheduleIntents[k] = cloneScheduleIntent(i)
	if err := s.saveLocked(); err != nil {
		if ok {
			s.data.ScheduleIntents[k] = prev
		} else {
			delete(s.data.ScheduleIntents, k)
		}
		return ScheduleIntent{}, err
	}
	return cloneScheduleIntent(i), nil
}
func (s *Store) RevokeScheduleIntent(g, p string, gen int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := intentKey(g, p)
	i, ok := s.data.ScheduleIntents[k]
	if !ok {
		return ErrNotFound
	}
	if gen > 0 && i.Generation != gen {
		return ErrConflict
	}
	if i.State == IntentRevoked {
		return nil
	}
	oldIntent := i
	i.State = IntentRevoked
	s.data.ScheduleIntents[k] = i
	if err := s.saveLocked(); err != nil {
		s.data.ScheduleIntents[k] = oldIntent
		return err
	}
	return nil
}

// ConfirmProjected fences the trigger projection to the exact pending intent.
func (s *Store) ConfirmProjected(g, p string, gen int64, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := intentKey(g, p)
	i, ok := s.data.ScheduleIntents[k]
	if !ok {
		return ErrNotFound
	}
	profile, ok := s.data.Profiles[i.AgentID]
	if !ok || !profile.Enabled || profile.CurrentGoalID != i.GoalID || profile.Revision != i.ProfileRevision {
		return ErrNotRunnable
	}
	if i.State != IntentPending || i.Generation != gen || i.Fingerprint != fingerprint {
		return ErrConflict
	}
	old := i
	i.State = IntentProjected
	s.data.ScheduleIntents[k] = i
	if err := s.saveLocked(); err != nil {
		s.data.ScheduleIntents[k] = old
		return err
	}
	return nil
}

// FinalizeRun derives all mutable state from the stored Goal/Run. It records
// usage even when the goal was paused/stopped, but only active profiles create
// a pending intent; projection is a separate CAS operation.
func (s *Store) FinalizeRun(in FinalizeInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finalizeRunLocked(in)
}

// finalizeRunLocked is the single durable commit path for terminal turns.
// Caller holds Store.mu. Every in-memory mutation is covered by one snapshot
// save and one complete rollback on failure.
func (s *Store) finalizeRunLocked(in FinalizeInput) error {
	observedTerminal := in.TerminalStatus != ""
	if in.TerminalStatus == "" {
		in.TerminalStatus = "completed"
	}
	if in.RunID == "" || in.Now.IsZero() || in.ActualTokens < 0 || in.ActualTokens > math.MaxInt64 {
		return errors.New("invalid_finalize_input")
	}
	var gid string
	var idx int
	for g, rs := range s.data.Runs {
		for n, r := range rs {
			if r.ID == in.RunID {
				gid = g
				idx = n
				break
			}
		}
		if gid != "" {
			break
		}
	}
	if gid == "" {
		return ErrNotFound
	}
	r := s.data.Runs[gid][idx]
	if r.FinishedAt != nil {
		if fp, ok := s.data.FinalizationFingerprints[in.RunID]; ok && fp != finalizeFingerprint(in) {
			return ErrConflict
		}
		if _, ok := s.data.FinalizationFingerprints[in.RunID]; !ok {
			return ErrConflict
		}
		return nil
	}
	g, ok := s.data.Goals[gid]
	if !ok {
		return ErrConflict
	}
	p, hasProfile := s.data.Profiles[g.AgentID]
	decisionErr := ValidateFinalDecision(in.Decision, DecisionValidationContext{Now: in.Now, MinInterval: time.Duration(g.MinWakeIntervalSeconds) * time.Second, ExpiresAt: g.ExpiresAt, RegisteredSource: in.RegisteredSource})
	decisionValid := decisionErr == nil
	managed := g.Managed || hasProfile
	configMatch := !g.Managed || (in.ExpectedConfigRevision > 0 && g.ConfigRevision == in.ExpectedConfigRevision) || (in.ExpectedConfigRevision == 0 && ((in.ExpectedGoalRevision > 0 && g.Revision == in.ExpectedGoalRevision) || (in.ExpectedGoalRevision == 0 && g.ConfigRevision == 0)))
	// The run's own Goal remains the accounting owner even after a new cycle is
	// bound to the profile. Configuration/profile revisions only fence business
	// state and intent application; they never erase old-cycle usage.
	accountGoal := true
	goalMatch := !managed || (hasProfile && p.CurrentGoalID == g.ID)
	fenceMatch := goalMatch && (!managed || configMatch && (in.ExpectedProfileRevision == 0 || p.Revision == in.ExpectedProfileRevision) && (in.ExpectedGoalRevision == 0 || g.Revision >= in.ExpectedGoalRevision))
	canApply := fenceMatch && (!managed || p.Enabled)
	canIntent := canApply && g.Status != StatusPaused && g.Status != StatusStopped && g.Status != StatusCompleted
	if canIntent && in.Purpose != "" {
		if old, exists := s.data.ScheduleIntents[intentKey(g.ID, in.Purpose)]; exists && old.Generation > in.Generation {
			// A late callback cannot replace or revoke a newer controller intent.
			canIntent = false
			canApply = false
		}
	}
	var next *ScheduleIntent
	if canIntent && in.TerminalStatus == "completed" && in.ActualTokens > 0 && decisionValid && (in.Decision.NextAction == NextAt || in.Decision.NextAction == NextEvent) {
		if in.Purpose == "" || in.Generation <= 0 {
			return errors.New("invalid_schedule_intent")
		}
		var filter map[string]any
		if in.Decision.Event != nil {
			filter = cloneDecisionMap(in.Decision.Event.Filter)
		}
		next = &ScheduleIntent{ID: in.RunID + ":" + in.Purpose, AgentID: g.AgentID, GoalID: g.ID, Purpose: in.Purpose, Generation: in.Generation, Decision: in.Decision.Clone(), DueAt: in.Decision.NextWakeAt, EventFilter: filter, State: IntentPending, ProfileRevision: p.Revision, UpdatedAt: in.Now}
		next.Fingerprint = intentFingerprint(*next)
		if old, exists := s.data.ScheduleIntents[intentKey(g.ID, in.Purpose)]; exists && old.Generation == in.Generation && old.Fingerprint != next.Fingerprint {
			return ErrConflict
		}
	}
	old := s.data
	old.Goals = cloneGoalsMap(s.data.Goals)
	old.Runs = cloneRunsMap(s.data.Runs)
	old.ScheduleIntents = cloneIntentsMap(s.data.ScheduleIntents)
	old.Usage = cloneUsageMap(s.data.Usage)
	old.UsageReceipts = cloneReceiptMap(s.data.UsageReceipts)
	old.FinalizationFingerprints = cloneStringMap(s.data.FinalizationFingerprints)
	alreadyCharged := false
	if _, exists := s.data.UsageReceipts[r.ID]; exists {
		alreadyCharged = true
	}
	if !alreadyCharged && in.ActualTokens > math.MaxInt64-g.TokensUsed {
		s.data = old
		return errors.New("usage_overflow")
	}
	if !alreadyCharged && accountGoal {
		g.TokensUsed += in.ActualTokens
	}
	if canIntent && decisionValid && in.TerminalStatus == "completed" && g.Status != StatusStopped && g.Status != StatusPaused {
		switch in.Decision.Outcome {
		case OutcomeCompleted:
			g.Status, g.StatusReason = StatusCompleted, ""
		case OutcomeProgress, OutcomeBlocked, OutcomeNoChange:
			if observedTerminal {
				g.Status, g.StatusReason = StatusWaiting, ""
			}
		}
		if in.Decision.NextAction == NextNeedsInput {
			g.Status, g.StatusReason = StatusPaused, "needs_input"
		} else if in.Decision.NextAction == NextNone && g.Status != StatusCompleted {
			g.Status, g.StatusReason = StatusWaiting, "no_wake_requested"
		}
	}
	if !decisionValid && in.TerminalStatus == "completed" && managed && canApply && g.Status != StatusStopped && g.Status != StatusCompleted {
		g.Status = StatusPaused
		g.StatusReason = decisionErr.Error()
	}
	if in.TerminalStatus == "completed" && decisionErr != nil && canIntent {
		if oldIntent, exists := s.data.ScheduleIntents[intentKey(g.ID, in.Purpose)]; exists {
			oldIntent.State = IntentRevoked
			s.data.ScheduleIntents[intentKey(g.ID, in.Purpose)] = oldIntent
		}
	}
	if in.TerminalStatus != "completed" && canApply && g.Status != StatusStopped && g.Status != StatusPaused && g.Status != StatusCompleted {
		g.Status = StatusFailed
		if strings.TrimSpace(in.TerminalReason) != "" {
			g.StatusReason = in.TerminalReason
		}
	}
	if in.ActualTokens == 0 {
		if canApply && g.Status != StatusStopped {
			g.Status = StatusPaused
			g.StatusReason = "usage_unknown"
		}
		if in.TerminalStatus == "completed" {
			r.Status = "unknown"
		}
	}
	// Legacy non-managed goals have no profile and keep their historical
	// evidence/checkpoint projection semantics.
	if !g.Managed && !hasProfile && in.TerminalStatus == "completed" && (g.Status == StatusActive || g.Status == StatusWaiting) {
		if checkpointProvesCompletion(r.Checkpoint) {
			g.Status, g.StatusReason = StatusCompleted, ""
		} else {
			g.Status, g.StatusReason = StatusWaiting, ""
		}
	}
	if !g.Managed && !hasProfile && in.TerminalStatus == "completed" && g.Status == StatusWaiting {
		progress := checkpointProgress(r.Checkpoint)
		streak := 1
		for i := len(s.data.Runs[gid]) - 2; i >= 0 && streak < 2; i-- {
			prior := s.data.Runs[gid][i]
			if prior.Status != "completed" {
				continue
			}
			if checkpointProgress(prior.Checkpoint) == progress {
				streak++
			} else {
				break
			}
		}
		if streak >= 2 {
			g.Status, g.StatusReason = StatusPaused, "no_progress"
		}
	}
	if accountGoal {
		g.Revision++
		g.UpdatedAt = in.Now.UTC()
	}
	r.TokensUsed = in.ActualTokens
	r.Status = in.TerminalStatus
	if r.Status == "" {
		r.Status = "completed"
	}
	if in.ActualTokens == 0 {
		r.Status = "unknown"
	}
	r.Reason = in.TerminalReason
	t := in.Now.UTC()
	r.FinishedAt = &t
	if accountGoal {
		s.data.Goals[gid] = g
	}
	s.data.Runs[gid][idx] = r
	if in.ActualTokens > 0 {
		if _, _, err := s.recordRunUsageLocked(g.AgentID, r.ID, in.ActualTokens, 0, 0, in.Now); err != nil {
			s.data = old
			return err
		}
	} else {
		u := s.data.Usage[g.AgentID]
		u.AgentID, u.Unknown, u.UnknownReason, u.UpdatedAt = g.AgentID, true, "usage reconciliation required", in.Now.UTC()
		s.data.Usage[g.AgentID] = u
	}
	if next != nil {
		s.data.ScheduleIntents[intentKey(g.ID, in.Purpose)] = *next
	} else if canIntent && in.Purpose != "" {
		if oldIntent, exists := s.data.ScheduleIntents[intentKey(g.ID, in.Purpose)]; exists {
			oldIntent.State = IntentRevoked
			s.data.ScheduleIntents[intentKey(g.ID, in.Purpose)] = oldIntent
		}
	}
	s.data.FinalizationFingerprints[in.RunID] = finalizeFingerprint(in)
	if err := s.saveLocked(); err != nil {
		s.data = old
		return err
	}
	return nil
}
func validateIntent(i ScheduleIntent) error {
	if i.ID == "" || i.AgentID == "" || i.GoalID == "" || i.Purpose == "" || i.Generation <= 0 {
		return errors.New("invalid_schedule_intent")
	}
	switch i.State {
	case IntentPending, IntentProjected, IntentRevoked:
	default:
		return errors.New("invalid_schedule_intent_state")
	}
	return nil
}
func sameIntent(a, b ScheduleIntent) bool {
	return a.Fingerprint != "" && a.Fingerprint == b.Fingerprint && a.State == b.State
}
func intentFingerprint(i ScheduleIntent) string {
	b, _ := json.Marshal(struct {
		A, G, P string
		N       int64
		D       FinalDecision
		T       *time.Time
		F       map[string]any
	}{i.AgentID, i.GoalID, i.Purpose, i.Generation, i.Decision, i.DueAt, i.EventFilter})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func finalizeFingerprint(i FinalizeInput) string {
	b, _ := json.Marshal(struct {
		Run      string
		Tokens   int64
		Decision FinalDecision
	}{i.RunID, i.ActualTokens, i.Decision})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func cloneGoalsMap(m map[string]Goal) map[string]Goal {
	o := make(map[string]Goal, len(m))
	for k, v := range m {
		o[k] = cloneGoal(v)
	}
	return o
}
func cloneRunsMap(m map[string][]Run) map[string][]Run {
	o := make(map[string][]Run, len(m))
	for k, v := range m {
		o[k] = cloneRuns(v)
	}
	return o
}
func cloneIntentsMap(m map[string]ScheduleIntent) map[string]ScheduleIntent {
	o := make(map[string]ScheduleIntent, len(m))
	for k, v := range m {
		o[k] = cloneScheduleIntent(v)
	}
	return o
}
func cloneUsageMap(m map[string]AgentUsage) map[string]AgentUsage {
	o := make(map[string]AgentUsage, len(m))
	for k, v := range m {
		o[k] = v
	}
	return o
}
func cloneReceiptMap(m map[string]UsageReceipt) map[string]UsageReceipt {
	o := make(map[string]UsageReceipt, len(m))
	for k, v := range m {
		o[k] = v
	}
	return o
}
func cloneStringMap(m map[string]string) map[string]string {
	o := make(map[string]string, len(m))
	for k, v := range m {
		o[k] = v
	}
	return o
}
