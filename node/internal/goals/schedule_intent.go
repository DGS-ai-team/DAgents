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
	if i.Fingerprint == "" {
		i.Fingerprint = intentFingerprint(i)
	}
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
	i.State = IntentRevoked
	s.data.ScheduleIntents[k] = i
	if err := s.saveLocked(); err != nil {
		i.State = IntentPending
		s.data.ScheduleIntents[k] = i
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
	if in.RunID == "" || in.ExpectedGoalRevision <= 0 || in.ExpectedProfileRevision <= 0 || in.Now.IsZero() || in.ActualTokens < 0 || in.ActualTokens > math.MaxInt64 {
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
	p, ok := s.data.Profiles[g.AgentID]
	if !ok {
		return ErrConflict
	}
	if p.CurrentGoalID != g.ID {
		return ErrConflict
	}
	decisionErr := ValidateFinalDecision(in.Decision, DecisionValidationContext{Now: in.Now, MinInterval: time.Duration(g.MinWakeIntervalSeconds) * time.Second, ExpiresAt: g.ExpiresAt, RegisteredSource: in.RegisteredSource})
	decisionValid := decisionErr == nil
	canIntent := p.Enabled && p.Revision == in.ExpectedProfileRevision && g.Revision == in.ExpectedGoalRevision && g.Status != StatusPaused && g.Status != StatusStopped && g.Status != StatusCompleted
	if canIntent && in.Purpose != "" {
		if old, exists := s.data.ScheduleIntents[intentKey(g.ID, in.Purpose)]; exists && old.Generation > in.Generation {
			// A late callback cannot replace or revoke a newer controller intent.
			canIntent = false
		}
	}
	var next *ScheduleIntent
	if canIntent && decisionValid && (in.Decision.NextAction == NextAt || in.Decision.NextAction == NextEvent) {
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
	if !alreadyCharged {
		g.TokensUsed += in.ActualTokens
	}
	if canIntent && decisionValid && g.Status != StatusStopped && g.Status != StatusPaused {
		switch in.Decision.Outcome {
		case OutcomeCompleted:
			g.Status = StatusCompleted
		case OutcomeBlocked, OutcomeNoChange:
			g.Status = StatusWaiting
		}
	}
	if !decisionValid && canIntent && g.Status == StatusActive {
		g.Status = StatusPaused
		g.StatusReason = decisionErr.Error()
	}
	g.Revision++
	g.UpdatedAt = in.Now.UTC()
	r.TokensUsed = in.ActualTokens
	r.Status = "completed"
	if decisionErr != nil {
		r.Reason = decisionErr.Error()
	}
	t := in.Now.UTC()
	r.FinishedAt = &t
	s.data.Goals[gid] = g
	s.data.Runs[gid][idx] = r
	if _, _, err := s.recordRunUsageLocked(g.AgentID, r.ID, in.ActualTokens, 0, 0, in.Now); err != nil {
		s.data = old
		return err
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
	return a.Fingerprint != "" && a.Fingerprint == b.Fingerprint
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
