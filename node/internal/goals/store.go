package goals

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("goal not found")
var ErrNotRunnable = errors.New("goal is not runnable")
var ErrConflict = errors.New("goal state conflict")
var ErrUsageUnknown = errors.New("agent usage is unknown")
var ErrEventSchedulingUnsupported = errors.New("event scheduling is not supported")

const CurrentSchemaVersion = 2

type disk struct {
	SchemaVersion            int                           `json:"schema_version,omitempty"`
	Goals                    map[string]Goal               `json:"goals"`
	Runs                     map[string][]Run              `json:"runs"`
	Profiles                 map[string]AutoProfile        `json:"profiles,omitempty"`
	Usage                    map[string]AgentUsage         `json:"usage,omitempty"`
	UsageReceipts            map[string]UsageReceipt       `json:"usage_receipts,omitempty"`
	MigrationIssues          map[string]MigrationIssue     `json:"migration_issues,omitempty"`
	ScheduleIntents          map[string]ScheduleIntent     `json:"schedule_intents,omitempty"`
	FinalizationFingerprints map[string]string             `json:"finalization_fingerprints,omitempty"`
	MaintenanceReceipts      map[string]MaintenanceReceipt `json:"maintenance_receipts,omitempty"`
}
type Store struct {
	mu                sync.RWMutex
	path              string
	data              disk
	registeredSources map[string]map[string]bool
}

// SetRegisteredSources supplies the current owner-independent source names
// used when validating event decisions at lifecycle finalization.
func (s *Store) SetRegisteredSources(sources map[string]map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registeredSources = make(map[string]map[string]bool, len(sources))
	for agent, list := range sources {
		s.registeredSources[agent] = make(map[string]bool, len(list))
		for source, enabled := range list {
			s.registeredSources[agent][source] = enabled
		}
	}
}

func cloneCheckpoint(cp *Checkpoint) *Checkpoint {
	if cp == nil {
		return nil
	}
	out := *cp
	out.Completed = append([]string(nil), cp.Completed...)
	out.NextSteps = append([]string(nil), cp.NextSteps...)
	out.Evidence = append([]string(nil), cp.Evidence...)
	out.Artifacts = append([]string(nil), cp.Artifacts...)
	if cp.Decision != nil {
		d := cp.Decision.Clone()
		out.Decision = &d
	}
	if cp.NextWakeAt != nil {
		t := *cp.NextWakeAt
		out.NextWakeAt = &t
	}
	return &out
}
func cloneGoal(g Goal) Goal {
	if g.ExpiresAt != nil {
		t := *g.ExpiresAt
		g.ExpiresAt = &t
	}
	if g.NextWakeAt != nil {
		t := *g.NextWakeAt
		g.NextWakeAt = &t
	}
	if g.ScheduleOccurrence != nil {
		t := *g.ScheduleOccurrence
		g.ScheduleOccurrence = &t
	}
	if g.DispatchAt != nil {
		t := *g.DispatchAt
		g.DispatchAt = &t
	}
	g.LastCheckpoint = cloneCheckpoint(g.LastCheckpoint)
	return g
}
func cloneRun(r Run) Run {
	if r.FinishedAt != nil {
		t := *r.FinishedAt
		r.FinishedAt = &t
	}
	r.Checkpoint = cloneCheckpoint(r.Checkpoint)
	return r
}
func cloneRuns(in []Run) []Run {
	out := make([]Run, len(in))
	for i, r := range in {
		out[i] = cloneRun(r)
	}
	return out
}

// WakeFunc must enqueue the input into the goal's dedicated Session and return
// the authoritative delivery/Turn identity. Returning an empty ID is failure.
type WakeFunc func(context.Context, Goal, Run) (string, error)

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, data: disk{SchemaVersion: CurrentSchemaVersion, Goals: map[string]Goal{}, Runs: map[string][]Run{}, Profiles: map[string]AutoProfile{}, Usage: map[string]AgentUsage{}, UsageReceipts: map[string]UsageReceipt{}, MaintenanceReceipts: map[string]MaintenanceReceipt{}, MigrationIssues: map[string]MigrationIssue{}, ScheduleIntents: map[string]ScheduleIntent{}, FinalizationFingerprints: map[string]string{}}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var loaded disk
	if err = json.Unmarshal(b, &loaded); err != nil {
		return nil, fmt.Errorf("parse goals store: %w", err)
	}
	if loaded.SchemaVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("unsupported goals schema version %d", loaded.SchemaVersion)
	}
	if loaded.SchemaVersion == 0 && path != "" {
		backup := path + ".v1.bak"
		if _, statErr := os.Stat(backup); os.IsNotExist(statErr) {
			if backupErr := os.WriteFile(backup, b, 0600); backupErr != nil {
				return nil, fmt.Errorf("backup legacy goals store: %w", backupErr)
			}
		}
	}
	s.data = loaded
	s.data.SchemaVersion = CurrentSchemaVersion
	if s.data.Goals == nil {
		s.data.Goals = map[string]Goal{}
	}
	if s.data.Runs == nil {
		s.data.Runs = map[string][]Run{}
	}
	if s.data.Profiles == nil {
		s.data.Profiles = map[string]AutoProfile{}
	}
	if s.data.Usage == nil {
		s.data.Usage = map[string]AgentUsage{}
	}
	if s.data.UsageReceipts == nil {
		s.data.UsageReceipts = map[string]UsageReceipt{}
	}
	if s.data.MaintenanceReceipts == nil {
		s.data.MaintenanceReceipts = map[string]MaintenanceReceipt{}
	}
	if s.data.MigrationIssues == nil {
		s.data.MigrationIssues = map[string]MigrationIssue{}
	}
	if s.data.ScheduleIntents == nil {
		s.data.ScheduleIntents = map[string]ScheduleIntent{}
	}
	if s.data.FinalizationFingerprints == nil {
		s.data.FinalizationFingerprints = map[string]string{}
	}
	// A process restart cannot prove whether an in-flight Turn had side effects.
	// Fail closed and require an explicit wake after inspection.
	for id, rs := range s.data.Runs {
		for i := range rs {
			if rs[i].FinishedAt == nil && (rs[i].Status == "running" || rs[i].Status == "waiting") {
				rs[i].Status = "unknown"
				rs[i].Reason = "runtime restart requires reconciliation"
				now := time.Now().UTC()
				rs[i].FinishedAt = &now
				rs[i].GoalID = id
				s.data.Runs[id] = rs
				g := s.data.Goals[id]
				if g.Status == StatusActive {
					g.Status = StatusPaused
					g.StatusReason = "runtime restart requires reconciliation"
					g.UpdatedAt = now
					s.data.Goals[id] = g
				}
			}
		}
	}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s.data, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
func (s *Store) List() []Goal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Goal, 0, len(s.data.Goals))
	for _, g := range s.data.Goals {
		out = append(out, cloneGoal(g))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}
func (s *Store) Get(id string) (Goal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.data.Goals[id]
	return cloneGoal(g), ok
}

func (s *Store) GetProfile(agentID string) (AutoProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.data.Profiles[strings.TrimSpace(agentID)]
	return p, ok
}

// SaveProfile creates or updates an explicit Auto profile. expectedRevision
// is zero only for first creation; updates use compare-and-swap semantics.
func (s *Store) SaveProfile(profile AutoProfile, expectedRevision int64, now time.Time) (AutoProfile, error) {
	agentID := strings.TrimSpace(profile.AgentID)
	if agentID == "" {
		return AutoProfile{}, fmt.Errorf("agent_id is required")
	}
	if profile.PlanMode != "" && profile.PlanMode != "one_shot" && profile.PlanMode != "recurring" {
		return AutoProfile{}, fmt.Errorf("invalid plan_mode")
	}
	if profile.Timezone != "" {
		if _, err := time.LoadLocation(profile.Timezone); err != nil {
			return AutoProfile{}, fmt.Errorf("invalid timezone: %w", err)
		}
	}
	if profile.BusinessTokenBudget < 0 || profile.MaintenanceTokenBudget < 0 || profile.TotalTokenBudget < 0 {
		return AutoProfile{}, fmt.Errorf("profile budgets cannot be negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.data.Profiles[agentID]
	if exists && old.Revision != expectedRevision {
		return AutoProfile{}, fmt.Errorf("%w: profile revision", ErrConflict)
	}
	if !exists && expectedRevision != 0 {
		return AutoProfile{}, fmt.Errorf("%w: profile does not exist", ErrConflict)
	}
	profile.AgentID = agentID
	profile.Revision = old.Revision + 1
	profile.UpdatedAt = now.UTC()
	if exists && profile.CurrentGoalID == "" {
		profile.CurrentGoalID = old.CurrentGoalID
	}
	if exists && profile.CurrentGoalID != old.CurrentGoalID {
		return AutoProfile{}, fmt.Errorf("%w: current_goal_id is managed by cycle creation", ErrConflict)
	}
	if profile.CurrentGoalID != "" {
		g, ok := s.data.Goals[profile.CurrentGoalID]
		if !ok || !g.Managed || strings.TrimSpace(g.AgentID) != agentID {
			return AutoProfile{}, fmt.Errorf("%w: current goal does not belong to profile", ErrConflict)
		}
	}
	s.data.Profiles[agentID] = profile
	if err := s.saveLocked(); err != nil {
		if exists {
			s.data.Profiles[agentID] = old
		} else {
			delete(s.data.Profiles, agentID)
		}
		return AutoProfile{}, err
	}
	return profile, nil
}

func (s *Store) GetUsage(agentID string) (AgentUsage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.data.Usage[strings.TrimSpace(agentID)]
	return u, ok
}

func (s *Store) MigrationIssues() []MigrationIssue {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MigrationIssue, 0, len(s.data.MigrationIssues))
	for _, issue := range s.data.MigrationIssues {
		issue.GoalIDs = append([]string(nil), issue.GoalIDs...)
		out = append(out, issue)
	}
	return out
}

// RecordUsage adds authoritative usage to the Agent aggregate atomically.
func (s *Store) RecordUsage(agentID string, business, maintenance, unknown int64, now time.Time) (AgentUsage, error) {
	if business < 0 || maintenance < 0 || unknown < 0 {
		return AgentUsage{}, fmt.Errorf("usage increments cannot be negative")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return AgentUsage{}, fmt.Errorf("agent_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.data.Usage[agentID]
	old := u
	if business > math.MaxInt64-u.BusinessTokens || maintenance > math.MaxInt64-u.MaintenanceTokens || unknown > math.MaxInt64-u.UnknownTokens {
		return AgentUsage{}, fmt.Errorf("usage overflow")
	}
	u.AgentID = agentID
	u.BusinessTokens += business
	u.MaintenanceTokens += maintenance
	u.UnknownTokens += unknown
	if unknown > 0 {
		u.Unknown = true
		u.UnknownReason = "usage reconciliation required"
	}
	u.UpdatedAt = now.UTC()
	s.data.Usage[agentID] = u
	if err := s.saveLocked(); err != nil {
		s.data.Usage[agentID] = old
		return AgentUsage{}, err
	}
	return u, nil
}

// RecordRunUsage accounts a run exactly once using its durable run ID.
func (s *Store) RecordRunUsage(agentID, runID string, business, maintenance, unknown int64, now time.Time) (AgentUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agentID, runID = strings.TrimSpace(agentID), strings.TrimSpace(runID)
	fingerprint := fmt.Sprintf("%d:%d:%d", business, maintenance, unknown)
	if receipt, ok := s.data.UsageReceipts[runID]; ok {
		if receipt.AgentID != agentID || receipt.Fingerprint != fingerprint {
			return AgentUsage{}, fmt.Errorf("%w: duplicate run usage differs", ErrConflict)
		}
		return s.data.Usage[agentID], nil
	}
	u, old, err := s.recordRunUsageLocked(agentID, runID, business, maintenance, unknown, now)
	if err != nil {
		return AgentUsage{}, err
	}
	if err := s.saveLocked(); err != nil {
		s.data.Usage[strings.TrimSpace(agentID)] = old
		delete(s.data.UsageReceipts, strings.TrimSpace(runID))
		return AgentUsage{}, err
	}
	return u, nil
}

func (s *Store) recordRunUsageLocked(agentID, runID string, business, maintenance, unknown int64, now time.Time) (AgentUsage, AgentUsage, error) {
	agentID, runID = strings.TrimSpace(agentID), strings.TrimSpace(runID)
	if agentID == "" || runID == "" || business < 0 || maintenance < 0 || unknown < 0 {
		return AgentUsage{}, AgentUsage{}, fmt.Errorf("invalid run usage")
	}
	fingerprint := fmt.Sprintf("%d:%d:%d", business, maintenance, unknown)
	if receipt, ok := s.data.UsageReceipts[runID]; ok {
		if receipt.AgentID != agentID || receipt.Fingerprint != fingerprint {
			return AgentUsage{}, AgentUsage{}, fmt.Errorf("%w: duplicate run usage differs", ErrConflict)
		}
		return s.data.Usage[agentID], s.data.Usage[agentID], nil
	}
	old := s.data.Usage[agentID]
	u := old
	if business > math.MaxInt64-u.BusinessTokens || maintenance > math.MaxInt64-u.MaintenanceTokens || unknown > math.MaxInt64-u.UnknownTokens {
		return AgentUsage{}, AgentUsage{}, fmt.Errorf("usage overflow")
	}
	u.AgentID, u.BusinessTokens, u.MaintenanceTokens, u.UnknownTokens = agentID, u.BusinessTokens+business, u.MaintenanceTokens+maintenance, u.UnknownTokens+unknown
	if unknown > 0 {
		u.Unknown, u.UnknownReason = true, "usage reconciliation required"
	}
	u.UpdatedAt = now.UTC()
	s.data.Usage[agentID] = u
	s.data.UsageReceipts[runID] = UsageReceipt{AgentID: agentID, Fingerprint: fingerprint}
	return u, old, nil
}

// MigrateAutoProfiles explicitly upgrades legacy managed goals only when the
// caller supplies the current, validated Auto Agent IDs. Ambiguous histories
// are left untouched for operator resolution rather than guessed.
func (s *Store) MigrateAutoProfiles(validAutoIDs map[string]bool, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldProfiles := make(map[string]AutoProfile, len(s.data.Profiles))
	for k, v := range s.data.Profiles {
		oldProfiles[k] = v
	}
	oldUsage := make(map[string]AgentUsage, len(s.data.Usage))
	for k, v := range s.data.Usage {
		oldUsage[k] = v
	}
	oldIssues := make(map[string]MigrationIssue, len(s.data.MigrationIssues))
	for k, v := range s.data.MigrationIssues {
		oldIssues[k] = v
	}
	changed := 0
	dirty := false
	for agentID := range validAutoIDs {
		if !validAutoIDs[agentID] || s.data.Profiles[agentID].AgentID != "" {
			continue
		}
		var candidates []Goal
		for _, g := range s.data.Goals {
			if g.Managed && g.AgentID == agentID {
				candidates = append(candidates, g)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		active := 0
		current := Goal{}
		for _, g := range candidates {
			if g.Status != StatusCompleted && g.Status != StatusStopped {
				active++
				current = g
			}
		}
		if active > 1 {
			ids := make([]string, 0, len(candidates))
			for _, g := range candidates {
				ids = append(ids, g.ID)
				if g.Status != StatusPaused && g.Status != StatusCompleted && g.Status != StatusStopped {
					g.Status = StatusPaused
					g.StatusReason = "migration_ambiguous_cycles"
					g.Revision++
					g.UpdatedAt = now.UTC()
					s.data.Goals[g.ID] = g
				}
			}
			s.data.MigrationIssues[agentID] = MigrationIssue{AgentID: agentID, Reason: "multiple non-terminal managed cycles require explicit selection", GoalIDs: ids, CreatedAt: now.UTC()}
			dirty = true
			continue
		}
		p := AutoProfile{AgentID: agentID, Revision: 1, Enabled: current.Status == StatusActive || current.Status == StatusWaiting, CurrentGoalID: current.ID, RoleObjective: current.Objective, RoleBoundaries: current.Acceptance, PlanMode: "one_shot", UpdatedAt: now.UTC()}
		s.data.Profiles[agentID] = p
		usage := s.data.Usage[agentID]
		usage.AgentID, usage.UpdatedAt = agentID, now.UTC()
		legacyTotal := int64(0)
		for _, g := range candidates {
			runTotal := int64(0)
			for _, run := range s.data.Runs[g.ID] {
				if run.TokensUsed > 0 {
					if runTotal > math.MaxInt64-run.TokensUsed {
						usage.Unknown = true
						usage.UnknownReason = "legacy usage overflow"
					} else {
						runTotal += run.TokensUsed
					}
				}
				if run.Status == "unknown" {
					usage.Unknown = true
					usage.UnknownReason = "legacy run requires reconciliation"
				}
			}
			contribution := runTotal
			if g.TokensUsed > contribution {
				usage.Unknown = true
				usage.UnknownReason = "legacy goal usage exceeds run totals"
				contribution = g.TokensUsed
			}
			if contribution > 0 {
				if legacyTotal > math.MaxInt64-contribution {
					usage.Unknown = true
					usage.UnknownReason = "legacy usage overflow"
				} else {
					legacyTotal += contribution
				}
			}
		}
		if usage.BusinessTokens < legacyTotal {
			usage.BusinessTokens = legacyTotal
		}
		s.data.Usage[agentID] = usage
		changed++
	}
	if changed == 0 && !dirty {
		return 0, nil
	}
	if err := s.saveLocked(); err != nil {
		s.data.Profiles, s.data.Usage, s.data.MigrationIssues = oldProfiles, oldUsage, oldIssues
		return 0, err
	}
	return changed, nil
}

func cycleFingerprint(in CreateInput, idempotencyKey string) string {
	b, _ := json.Marshal(struct {
		Title, Objective, Acceptance, AgentID            string
		MaxRuns, TurnTokenBudget, MinWakeIntervalSeconds int
		TokenBudget                                      int64
		ExpiresAt                                        *time.Time
		EnabledIntent                                    bool
	}{in.Title, in.Objective, in.Acceptance, in.AgentID, in.MaxRuns, in.TurnTokenBudget, in.MinWakeIntervalSeconds, in.TokenBudget, in.ExpiresAt, in.EnabledIntent})
	return fmt.Sprintf("%x", sha256.Sum256(append([]byte(idempotencyKey+"\x00"), b...)))
}

// Delete removes a just-created goal and its runs. API provisioning uses this
// only while rolling back a failed managed-resource setup.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.data.Goals, id)
	oldRuns, hadRuns := s.data.Runs[id]
	delete(s.data.Runs, id)
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = g
		if hadRuns {
			s.data.Runs[id] = oldRuns
		}
		return err
	}
	return nil
}

// RestoreConfiguration restores user-owned fields after a cross-store
// scheduler update fails. Runtime usage counters and run history are kept.
func (s *Store) RestoreConfiguration(id string, old Goal, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return ErrNotFound
	}
	previous := g
	g.Title, g.Objective, g.Acceptance = old.Title, old.Objective, old.Acceptance
	g.MaxRuns, g.TokenBudget, g.TurnTokenBudget = old.MaxRuns, old.TokenBudget, old.TurnTokenBudget
	g.ExpiresAt, g.NextWakeAt = old.ExpiresAt, old.NextWakeAt
	g.MinWakeIntervalSeconds, g.Status, g.StatusReason = old.MinWakeIntervalSeconds, old.Status, old.StatusReason
	g.UpdatedAt = now.UTC()
	g.Revision++
	if g.Managed {
		g.ConfigRevision++
	}
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = previous
		return err
	}
	return nil
}

// UpdateConfiguration changes only the user-owned autonomy limits and intent;
// accumulated runs and token usage remain untouched.
func (s *Store) UpdateConfiguration(id string, in CreateInput, now time.Time) (Goal, error) {
	return s.UpdateConfigurationAndStatus(id, in, nil, now)
}

// UpdateConfigurationAndStatus validates and persists configuration and an
// optional status transition under one lock, preserving usage counters.
func (s *Store) UpdateConfigurationAndStatus(id string, in CreateInput, status *Status, now time.Time) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return Goal{}, ErrNotFound
	}
	old := g
	if err := validateConfiguration(in, now, g); err != nil {
		return Goal{}, err
	}
	if strings.TrimSpace(in.Title) != "" {
		g.Title = strings.TrimSpace(in.Title)
	}
	if strings.TrimSpace(in.Objective) != "" {
		g.Objective = strings.TrimSpace(in.Objective)
	}
	if strings.TrimSpace(in.Acceptance) != "" {
		g.Acceptance = strings.TrimSpace(in.Acceptance)
	}
	if in.MaxRuns != 0 {
		g.MaxRuns = in.MaxRuns
	}
	if in.TokenBudget != 0 {
		g.TokenBudget = in.TokenBudget
	}
	if in.TurnTokenBudget != 0 {
		g.TurnTokenBudget = in.TurnTokenBudget
	}
	if in.ExpiresAt != nil {
		g.ExpiresAt = in.ExpiresAt
	}
	if in.MinWakeIntervalSeconds != 0 {
		g.MinWakeIntervalSeconds = in.MinWakeIntervalSeconds
	}
	if status != nil {
		if g.Status == StatusStopped || g.Status == StatusCompleted {
			return Goal{}, ErrNotRunnable
		}
		if *status == StatusActive {
			if g.MaxRuns > 0 && g.Runs >= g.MaxRuns || g.TokensUsed >= g.TokenBudget {
				return Goal{}, fmt.Errorf("goal budget exhausted")
			}
			if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
				return Goal{}, fmt.Errorf("goal expired")
			}
		}
		g.Status = *status
		if *status == StatusActive {
			g.StatusReason = ""
		}
	}
	g.UpdatedAt = now.UTC()
	g.Revision++
	if g.Managed {
		g.ConfigRevision++
	}
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = old
		return Goal{}, err
	}
	return cloneGoal(g), nil
}

func validateConfiguration(in CreateInput, now time.Time, g Goal) error {
	if in.MaxRuns < 0 || in.TokenBudget < 0 || in.TurnTokenBudget < 0 {
		return fmt.Errorf("budgets and max_runs cannot be negative")
	}
	if in.MaxRuns > 1000 || in.TurnTokenBudget > 100000 || in.TokenBudget > 10000000 {
		return fmt.Errorf("configuration exceeds limit")
	}
	if in.MinWakeIntervalSeconds < 0 || in.MinWakeIntervalSeconds > 30*24*60*60 || (in.MinWakeIntervalSeconds != 0 && in.MinWakeIntervalSeconds < 60) {
		return fmt.Errorf("invalid min_wake_interval_seconds")
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return fmt.Errorf("expires_at must be in the future")
	}
	if in.ExpiresAt != nil && g.NextWakeAt != nil && !in.ExpiresAt.After(*g.NextWakeAt) {
		return fmt.Errorf("expires_at is before next wake")
	}
	return nil
}

// UpdateProfileAndCycle applies an Auto profile edit and its current cycle
// configuration as one compare-and-swap transaction. Both records are
// validated while holding the store lock and persisted with one snapshot.
func (s *Store) UpdateProfileAndCycle(agentID, cycleID string, profile AutoProfile, expectedProfileRevision, expectedGoalRevision int64, in CreateInput, status *Status, now time.Time) (AutoProfile, Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agentID, cycleID = strings.TrimSpace(agentID), strings.TrimSpace(cycleID)
	oldP, ok := s.data.Profiles[agentID]
	if !ok {
		return AutoProfile{}, Goal{}, fmt.Errorf("%w: profile", ErrNotFound)
	}
	oldG, ok := s.data.Goals[cycleID]
	if !ok || !oldG.Managed || oldG.AgentID != agentID || oldP.CurrentGoalID != cycleID {
		return AutoProfile{}, Goal{}, fmt.Errorf("%w: cycle binding", ErrConflict)
	}
	if oldP.Revision != expectedProfileRevision || oldG.Revision != expectedGoalRevision {
		return AutoProfile{}, Goal{}, ErrConflict
	}
	if oldG.Status == StatusCompleted || oldG.Status == StatusStopped {
		return AutoProfile{}, Goal{}, ErrNotRunnable
	}
	for _, run := range s.data.Runs[cycleID] {
		if run.FinishedAt == nil || run.Status == "unknown" {
			return AutoProfile{}, Goal{}, fmt.Errorf("goal has an unresolved run")
		}
	}
	if err := validateConfiguration(in, now.UTC(), oldG); err != nil {
		return AutoProfile{}, Goal{}, err
	}
	if profile.PlanMode != "" && profile.PlanMode != "one_shot" && profile.PlanMode != "recurring" {
		return AutoProfile{}, Goal{}, fmt.Errorf("invalid plan_mode")
	}
	if profile.Timezone != "" {
		if _, err := time.LoadLocation(profile.Timezone); err != nil {
			return AutoProfile{}, Goal{}, err
		}
	}
	if profile.BusinessTokenBudget < 0 || profile.MaintenanceTokenBudget < 0 || profile.TotalTokenBudget < 0 {
		return AutoProfile{}, Goal{}, fmt.Errorf("profile budgets cannot be negative")
	}
	profile.AgentID, profile.CurrentGoalID = agentID, cycleID
	profile.Revision, profile.UpdatedAt = oldP.Revision+1, now.UTC()
	g := oldG
	if strings.TrimSpace(in.Objective) != "" {
		g.Objective = strings.TrimSpace(in.Objective)
	}
	if strings.TrimSpace(in.Acceptance) != "" {
		g.Acceptance = strings.TrimSpace(in.Acceptance)
	}
	if in.Title != "" {
		g.Title = strings.TrimSpace(in.Title)
	}
	if in.MaxRuns != 0 {
		g.MaxRuns = in.MaxRuns
	}
	if in.TokenBudget != 0 {
		g.TokenBudget = in.TokenBudget
	}
	if in.TurnTokenBudget != 0 {
		g.TurnTokenBudget = in.TurnTokenBudget
	}
	if in.MinWakeIntervalSeconds != 0 {
		g.MinWakeIntervalSeconds = in.MinWakeIntervalSeconds
	}
	if in.ExpiresAt != nil {
		t := in.ExpiresAt.UTC()
		g.ExpiresAt = &t
	}
	if status != nil {
		switch *status {
		case StatusActive, StatusPaused, StatusStopped, StatusCompleted, StatusWaiting, StatusFailed:
		default:
			return AutoProfile{}, Goal{}, fmt.Errorf("invalid goal status")
		}
		if oldG.Status == StatusCompleted || oldG.Status == StatusStopped {
			return AutoProfile{}, Goal{}, ErrNotRunnable
		}
		if *status == StatusActive {
			if (oldG.MaxRuns > 0 && oldG.Runs >= oldG.MaxRuns) || oldG.TokensUsed >= oldG.TokenBudget {
				return AutoProfile{}, Goal{}, fmt.Errorf("goal budget exhausted")
			}
			if oldG.ExpiresAt != nil && !now.Before(*oldG.ExpiresAt) {
				return AutoProfile{}, Goal{}, fmt.Errorf("goal expired")
			}
		}
		g.Status = *status
		if *status == StatusActive {
			g.StatusReason = ""
		}
	}
	// Configuration fencing is independent of the runtime revision, so a
	// callback from an already running turn cannot apply a newly edited plan.
	g.UpdatedAt, g.Revision = now.UTC(), oldG.Revision+1
	g.ConfigRevision = oldG.ConfigRevision + 1
	s.data.Profiles[agentID], s.data.Goals[cycleID] = profile, g
	if err := s.saveLocked(); err != nil {
		s.data.Profiles[agentID], s.data.Goals[cycleID] = oldP, oldG
		return AutoProfile{}, Goal{}, err
	}
	return profile, cloneGoal(g), nil
}

// ApplyAutoAction atomically applies lifecycle intent. Pause/disable are
// allowed while a run is active and never revive terminal cycles; enable does
// not resume a manually paused cycle.
func (s *Store) ApplyAutoAction(agentID, cycleID, action string, expectedProfileRevision, expectedGoalRevision int64, now time.Time) (AutoProfile, Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agentID, cycleID = strings.TrimSpace(agentID), strings.TrimSpace(cycleID)
	p, ok := s.data.Profiles[agentID]
	if !ok || p.Revision != expectedProfileRevision {
		return AutoProfile{}, Goal{}, ErrConflict
	}
	var g Goal
	hasGoal := cycleID != ""
	if hasGoal {
		var exists bool
		g, exists = s.data.Goals[cycleID]
		if !exists || !g.Managed || g.AgentID != agentID || p.CurrentGoalID != cycleID || g.Revision != expectedGoalRevision {
			return AutoProfile{}, Goal{}, ErrConflict
		}
	}
	if action != "pause_goal" && action != "resume_goal" && action != "disable_auto" && action != "enable_auto" {
		return AutoProfile{}, Goal{}, fmt.Errorf("invalid action")
	}
	if action == "resume_goal" || action == "enable_auto" {
		if !p.Enabled && action == "resume_goal" {
			return AutoProfile{}, Goal{}, fmt.Errorf("profile disabled")
		}
		if hasGoal {
			if action == "resume_goal" && (g.Status == StatusCompleted || g.Status == StatusStopped) {
				return AutoProfile{}, Goal{}, ErrNotRunnable
			}
			if action == "resume_goal" {
				if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
					return AutoProfile{}, Goal{}, fmt.Errorf("goal expired")
				}
				u := s.data.Usage[agentID]
				if u.Unknown || u.UnknownTokens > 0 {
					return AutoProfile{}, Goal{}, ErrUsageUnknown
				}
				for _, run := range s.data.Runs[g.ID] {
					if run.FinishedAt == nil || run.Status == "unknown" {
						return AutoProfile{}, Goal{}, fmt.Errorf("goal busy or usage unknown")
					}
				}
				if (g.MaxRuns > 0 && g.Runs >= g.MaxRuns) || g.TokensUsed >= g.TokenBudget {
					return AutoProfile{}, Goal{}, fmt.Errorf("goal budget exhausted")
				}
				if p.BusinessTokenBudget > 0 && u.BusinessTokens >= p.BusinessTokenBudget {
					return AutoProfile{}, Goal{}, fmt.Errorf("agent business token budget exhausted")
				}
				if p.TotalTokenBudget > 0 && (u.BusinessTokens >= p.TotalTokenBudget || u.MaintenanceTokens >= p.TotalTokenBudget-u.BusinessTokens) {
					return AutoProfile{}, Goal{}, fmt.Errorf("agent total token budget exhausted")
				}
			}
		}
	}
	var resumeKeys []string
	resumeSet := map[string]bool{}
	if hasGoal && action == "resume_goal" {
		for k, i := range s.data.ScheduleIntents {
			if i.AgentID != agentID || i.GoalID != cycleID || i.Purpose != "goal" || i.State != IntentRevoked || i.Decision.Outcome == OutcomeCompleted {
				continue
			}
			if i.Decision.NextAction == NextEvent {
				return AutoProfile{}, Goal{}, ErrEventSchedulingUnsupported
			}
			if i.Decision.NextAction != NextAt || i.DueAt == nil {
				continue
			}
			if i.Generation == math.MaxInt64 {
				return AutoProfile{}, Goal{}, ErrConflict
			}
			candidate := i.Decision.Clone()
			if !i.DueAt.After(now) {
				due := now.Add(time.Duration(g.MinWakeIntervalSeconds) * time.Second)
				candidate.NextWakeAt = &due
			}
			if err := ValidateFinalDecision(candidate, DecisionValidationContext{Now: now, MinInterval: time.Duration(g.MinWakeIntervalSeconds) * time.Second, ExpiresAt: g.ExpiresAt}); err == nil {
				resumeKeys = append(resumeKeys, k)
				resumeSet[k] = true
			}
		}
	}
	oldP, oldG := p, g
	oldIntents := map[string]ScheduleIntent{}
	for k, i := range s.data.ScheduleIntents {
		if i.AgentID == agentID && (action == "disable_auto" || (action == "pause_goal" && i.GoalID == cycleID) || (action == "resume_goal" && resumeSet[k])) {
			oldIntents[k] = i
			i.State = IntentRevoked
			i.UpdatedAt = now.UTC()
			s.data.ScheduleIntents[k] = i
		}
	}
	if action == "disable_auto" {
		p.Enabled = false
	}
	if action == "enable_auto" {
		p.Enabled = true
	}
	if hasGoal && (action == "pause_goal" || action == "disable_auto") && g.Status != StatusCompleted && g.Status != StatusStopped {
		g.Status, g.StatusReason = StatusPaused, "user_paused"
	}
	if hasGoal && action == "resume_goal" {
		g.Status, g.StatusReason = StatusActive, ""
	}
	p.Revision++
	p.UpdatedAt = now.UTC()
	if hasGoal && action == "resume_goal" {
		// Resuming an explicitly revoked schedule starts a fresh intent
		// generation. It never re-enables the consumed/revoked trigger in place.
		for _, k := range resumeKeys {
			i := s.data.ScheduleIntents[k]
			if i.DueAt != nil && !i.DueAt.After(now) {
				due := now.Add(time.Duration(g.MinWakeIntervalSeconds) * time.Second)
				i.DueAt = &due
				i.Decision.NextWakeAt = &due
			}
			i.Generation++
			i.ID = uuid.NewString() + ":" + i.Purpose
			i.State = IntentPending
			i.ProfileRevision = p.Revision
			i.UpdatedAt = now.UTC()
			i.Fingerprint = intentFingerprint(i)
			s.data.ScheduleIntents[k] = i
		}
	}
	if hasGoal {
		g.Revision++
		g.UpdatedAt = now.UTC()
	}
	s.data.Profiles[agentID] = p
	if hasGoal {
		s.data.Goals[cycleID] = g
	}
	if err := s.saveLocked(); err != nil {
		s.data.Profiles[agentID] = oldP
		if hasGoal {
			s.data.Goals[cycleID] = oldG
		}
		for k, i := range oldIntents {
			s.data.ScheduleIntents[k] = i
		}
		return AutoProfile{}, Goal{}, err
	}
	return p, cloneGoal(g), nil
}

// UpdateAutonomyIntent atomically updates the user intent for an existing
// autonomy Goal. Budget, run counters and scheduling limits are immutable here.
func (s *Store) UpdateAutonomyIntent(id string, objective string, acceptance string, nextWakeAt *time.Time, now time.Time) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return Goal{}, ErrNotFound
	}
	if strings.TrimSpace(objective) == "" || strings.TrimSpace(acceptance) == "" {
		return Goal{}, fmt.Errorf("objective and acceptance are required")
	}
	if nextWakeAt != nil {
		if nextWakeAt.Before(now) {
			return Goal{}, fmt.Errorf("next wake must be in the future")
		}
		if g.ExpiresAt != nil && nextWakeAt.After(*g.ExpiresAt) {
			return Goal{}, fmt.Errorf("next wake exceeds deadline")
		}
		if g.MinWakeIntervalSeconds > 0 && nextWakeAt.Sub(g.UpdatedAt) < time.Duration(g.MinWakeIntervalSeconds)*time.Second {
			return Goal{}, fmt.Errorf("next wake is before minimum interval")
		}
	}
	old := g
	g.Objective, g.Acceptance = strings.TrimSpace(objective), strings.TrimSpace(acceptance)
	if nextWakeAt != nil {
		t := nextWakeAt.UTC()
		g.NextWakeAt = &t
	}
	g.UpdatedAt = now.UTC()
	g.Revision++
	g.ConfigRevision++
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = old
		return Goal{}, err
	}
	return cloneGoal(g), nil
}

func (s *Store) BindSession(id, sessionID string, now time.Time) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return Goal{}, ErrNotFound
	}
	old := g
	g.SessionID = sessionID
	g.UpdatedAt = now.UTC()
	g.Revision++
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = old
		return Goal{}, err
	}
	return g, nil
}
func (s *Store) BindTrigger(id, triggerID string, now time.Time) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return Goal{}, ErrNotFound
	}
	old := g
	g.TriggerID = triggerID
	g.UpdatedAt = now.UTC()
	g.Revision++
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = old
		return Goal{}, err
	}
	return g, nil
}

func (s *Store) SetProvisionStatus(id, status string, now time.Time) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return Goal{}, ErrNotFound
	}
	if status != "pending" && status != "ready" && status != "failed" {
		return Goal{}, fmt.Errorf("invalid provision status")
	}
	old := g
	g.ProvisionStatus, g.UpdatedAt, g.Revision = status, now.UTC(), g.Revision+1
	if status == "failed" && g.Status == StatusPaused {
		g.StatusReason = "provisioning_failed"
	}
	if status == "ready" && g.StatusReason == "provisioning_failed" {
		g.StatusReason = ""
	}
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = old
		return Goal{}, err
	}
	return cloneGoal(g), nil
}
func (s *Store) Create(in CreateInput, now time.Time) (Goal, error) {
	return s.create(in, now, StatusActive)
}

// CreateManaged creates a goal with its initial enabled state persisted in the
// same write as the goal itself. Disabled creation never briefly becomes active.
func (s *Store) CreateManaged(in CreateInput, now time.Time, enabled bool) (Goal, error) {
	status := StatusPaused
	if enabled {
		status = StatusActive
	}
	in.Managed = true
	return s.create(in, now, status)
}

// CreateManagedCycle creates a new Auto business cycle and binds it to the
// profile in one snapshot write. It is the only storage entry point for
// multi-cycle callers; legacy CreateManaged remains for compatibility.
func (s *Store) CreateManagedCycle(in CreateInput, idempotencyKey string, expectedProfileRevision int64, now time.Time, enabled bool) (Goal, error) {
	in.Title, in.Objective, in.Acceptance, in.AgentID = strings.TrimSpace(in.Title), strings.TrimSpace(in.Objective), strings.TrimSpace(in.Acceptance), strings.TrimSpace(in.AgentID)
	if in.Objective == "" || in.Acceptance == "" || in.AgentID == "" {
		return Goal{}, fmt.Errorf("objective, acceptance and agent_id are required")
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return Goal{}, fmt.Errorf("idempotency key is required")
	}
	fingerprint := cycleFingerprint(in, idempotencyKey)
	if in.MaxRuns == 0 {
		in.MaxRuns = 12
	}
	if in.TurnTokenBudget == 0 {
		in.TurnTokenBudget = 10000
	}
	if in.TokenBudget == 0 {
		in.TokenBudget = 100000
	}
	if in.MaxRuns < 0 || in.MaxRuns > 1000 || in.TurnTokenBudget < 0 || in.TurnTokenBudget > 100000 || in.TokenBudget < 0 || in.TokenBudget > 10000000 {
		return Goal{}, fmt.Errorf("invalid cycle limits")
	}
	if in.MinWakeIntervalSeconds == 0 {
		in.MinWakeIntervalSeconds = 300
	}
	if in.MinWakeIntervalSeconds < 60 || in.MinWakeIntervalSeconds > 30*24*60*60 {
		return Goal{}, fmt.Errorf("invalid min_wake_interval_seconds")
	}
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, ok := s.data.Profiles[in.AgentID]
	if !ok {
		return Goal{}, fmt.Errorf("managed profile not found")
	}
	// Idempotent replay is resolved before active-cycle checks; map iteration
	// order must never make a retry fail because another cycle is encountered.
	for _, existing := range s.data.Goals {
		if existing.Managed && existing.AgentID == in.AgentID && existing.IdempotencyKey == idempotencyKey {
			if existing.CycleFingerprint != fingerprint {
				return Goal{}, fmt.Errorf("%w: idempotency payload", ErrConflict)
			}
			return cloneGoal(existing), nil
		}
	}
	if in.ExpiresAt == nil {
		t := now.Add(24 * time.Hour)
		in.ExpiresAt = &t
	}
	if !in.ExpiresAt.After(now) {
		return Goal{}, fmt.Errorf("expires_at must be in the future")
	}
	if profile.Revision != expectedProfileRevision {
		return Goal{}, fmt.Errorf("%w: profile revision", ErrConflict)
	}
	if enabled && !profile.Enabled {
		return Goal{}, fmt.Errorf("%w: profile is disabled", ErrConflict)
	}
	for _, existing := range s.data.Goals {
		if existing.Managed && existing.AgentID == in.AgentID && existing.Status != StatusCompleted && existing.Status != StatusStopped {
			return Goal{}, fmt.Errorf("%w: active cycle exists", ErrConflict)
		}
		if existing.AgentID == in.AgentID {
			for _, run := range s.data.Runs[existing.ID] {
				if run.Status == "unknown" {
					return Goal{}, ErrUsageUnknown
				}
			}
		}
	}
	if usage := s.data.Usage[in.AgentID]; usage.Unknown || usage.UnknownTokens > 0 {
		return Goal{}, ErrUsageUnknown
	}
	usage := s.data.Usage[in.AgentID]
	if profile.BusinessTokenBudget > 0 && usage.BusinessTokens >= profile.BusinessTokenBudget {
		return Goal{}, fmt.Errorf("agent business token budget exhausted")
	}
	if profile.MaintenanceTokenBudget > 0 && usage.MaintenanceTokens >= profile.MaintenanceTokenBudget {
		return Goal{}, fmt.Errorf("agent maintenance token budget exhausted")
	}
	if profile.TotalTokenBudget > 0 && (usage.BusinessTokens >= profile.TotalTokenBudget || usage.MaintenanceTokens >= profile.TotalTokenBudget-usage.BusinessTokens) {
		return Goal{}, fmt.Errorf("agent total token budget exhausted")
	}
	seq := 0
	for _, existing := range s.data.Goals {
		if existing.AgentID == in.AgentID && existing.CycleSequence >= seq {
			seq = existing.CycleSequence + 1
		}
	}
	if seq == 0 {
		seq = 1
	}
	status := StatusPaused
	if enabled && profile.Enabled {
		status = StatusActive
	}
	g := Goal{ID: uuid.NewString(), Title: in.Title, Objective: in.Objective, Acceptance: in.Acceptance, AgentID: in.AgentID, Managed: true, Status: status, ProvisionStatus: "pending", EnableIntent: enabled, MaxRuns: in.MaxRuns, TokenBudget: in.TokenBudget, TurnTokenBudget: in.TurnTokenBudget, ExpiresAt: in.ExpiresAt, MinWakeIntervalSeconds: in.MinWakeIntervalSeconds, CycleSequence: seq, ProfileRevision: profile.Revision, ConfigRevision: 1, Revision: 1, IdempotencyKey: idempotencyKey, CycleFingerprint: fingerprint, CreatedAt: now, UpdatedAt: now}
	next := now.Add(time.Duration(in.MinWakeIntervalSeconds) * time.Second)
	g.NextWakeAt = &next
	oldProfile := profile
	gID := g.ID
	s.data.Goals[gID] = g
	profile.CurrentGoalID = gID
	profile.Revision++
	profile.UpdatedAt = now
	s.data.Profiles[in.AgentID] = profile
	if err := s.saveLocked(); err != nil {
		delete(s.data.Goals, gID)
		s.data.Profiles[in.AgentID] = oldProfile
		return Goal{}, err
	}
	return cloneGoal(g), nil
}

func (s *Store) create(in CreateInput, now time.Time, status Status) (Goal, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Objective = strings.TrimSpace(in.Objective)
	in.Acceptance = strings.TrimSpace(in.Acceptance)
	in.AgentID = strings.TrimSpace(in.AgentID)
	if in.Objective == "" || in.Acceptance == "" || in.AgentID == "" {
		return Goal{}, fmt.Errorf("objective, acceptance and agent_id are required")
	}
	if in.MaxRuns < 0 || in.TokenBudget < 0 || in.TurnTokenBudget < 0 {
		return Goal{}, fmt.Errorf("budgets and max_runs cannot be negative")
	}
	if in.MaxRuns == 0 {
		in.MaxRuns = 12
	}
	if in.MaxRuns > 1000 {
		return Goal{}, fmt.Errorf("max_runs exceeds limit")
	}
	if in.TurnTokenBudget == 0 {
		in.TurnTokenBudget = 10000
	}
	if in.TurnTokenBudget > 100000 {
		return Goal{}, fmt.Errorf("turn_token_budget exceeds limit")
	}
	if in.TokenBudget == 0 {
		in.TokenBudget = 100000
	}
	if in.TokenBudget > 10000000 {
		return Goal{}, fmt.Errorf("token_budget exceeds limit")
	}
	if in.ExpiresAt == nil {
		t := now.Add(24 * time.Hour)
		in.ExpiresAt = &t
	}
	if !in.ExpiresAt.After(now) {
		return Goal{}, fmt.Errorf("expires_at must be in the future")
	}
	if in.MinWakeIntervalSeconds == 0 {
		in.MinWakeIntervalSeconds = 300
	}
	if in.MinWakeIntervalSeconds < 60 {
		return Goal{}, fmt.Errorf("min_wake_interval_seconds must be >= 60")
	}
	if in.MinWakeIntervalSeconds > 30*24*60*60 {
		return Goal{}, fmt.Errorf("min_wake_interval_seconds exceeds 30 days")
	}
	now = now.UTC()
	next := now.Add(time.Duration(in.MinWakeIntervalSeconds) * time.Second)
	g := Goal{ID: uuid.NewString(), Title: in.Title, Objective: in.Objective, Acceptance: in.Acceptance, AgentID: in.AgentID, Managed: in.Managed, SessionID: in.SessionID, Status: status, MaxRuns: in.MaxRuns, TokenBudget: in.TokenBudget, TurnTokenBudget: in.TurnTokenBudget, ExpiresAt: in.ExpiresAt, MinWakeIntervalSeconds: in.MinWakeIntervalSeconds, NextWakeAt: &next, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Goals[g.ID] = g
	if err := s.saveLocked(); err != nil {
		delete(s.data.Goals, g.ID)
		return Goal{}, err
	}
	return cloneGoal(g), nil
}
func (s *Store) SetStatus(id string, status Status, now time.Time) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return Goal{}, ErrNotFound
	}
	if g.Status == StatusStopped || g.Status == StatusCompleted {
		return g, ErrNotRunnable
	}
	if status == StatusActive {
		if g.MaxRuns > 0 && g.Runs >= g.MaxRuns {
			return Goal{}, fmt.Errorf("goal run limit exhausted")
		}
		if g.TokensUsed >= g.TokenBudget {
			return Goal{}, fmt.Errorf("goal budget exhausted")
		}
		if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
			return Goal{}, fmt.Errorf("goal expired")
		}
		for _, run := range s.data.Runs[id] {
			if run.Status == "unknown" {
				return Goal{}, fmt.Errorf("goal usage unknown; reconcile before resume")
			}
		}
	}
	old := g
	g.Status = status
	if status == StatusActive {
		g.StatusReason = ""
	}
	g.UpdatedAt = now.UTC()
	g.Revision++
	if g.Managed {
		g.ConfigRevision++
	}
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = old
		return Goal{}, err
	}
	return g, nil
}
func (s *Store) StartRun(id, reason string, now time.Time) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	if g.Status != StatusActive && g.Status != StatusWaiting {
		return Run{}, ErrNotRunnable
	}
	profile := s.data.Profiles[g.AgentID]
	usage := s.data.Usage[g.AgentID]
	if profile.AgentID != "" {
		if _, issue := s.data.MigrationIssues[g.AgentID]; issue {
			return Run{}, ErrUsageUnknown
		}
		if !profile.Enabled || profile.CurrentGoalID != id {
			return Run{}, ErrNotRunnable
		}
		if usage.Unknown || usage.UnknownTokens > 0 {
			return Run{}, ErrUsageUnknown
		}
		for _, receipt := range s.data.MaintenanceReceipts {
			if receipt.AgentID == g.AgentID && receipt.Status == "pending" {
				return Run{}, fmt.Errorf("%w: maintenance reservation pending", ErrNotRunnable)
			}
		}
	}
	if profile.BusinessTokenBudget > 0 && usage.BusinessTokens >= profile.BusinessTokenBudget {
		old := g
		g.Status, g.StatusReason = StatusPaused, "agent_business_budget_exhausted"
		g.Revision++
		s.data.Goals[id] = g
		if err := s.saveLocked(); err != nil {
			s.data.Goals[id] = old
			return Run{}, err
		}
		return Run{}, fmt.Errorf("agent business token budget exhausted")
	}
	if profile.TotalTokenBudget > 0 && (usage.BusinessTokens >= profile.TotalTokenBudget || usage.MaintenanceTokens >= profile.TotalTokenBudget-usage.BusinessTokens) {
		old := g
		g.Status, g.StatusReason = StatusPaused, "agent_total_budget_exhausted"
		g.Revision++
		s.data.Goals[id] = g
		if err := s.saveLocked(); err != nil {
			s.data.Goals[id] = old
			return Run{}, err
		}
		return Run{}, fmt.Errorf("agent total token budget exhausted")
	}
	if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
		old := g
		g.Status = StatusPaused
		g.StatusReason = "expired"
		s.data.Goals[id] = g
		if err := s.saveLocked(); err != nil {
			s.data.Goals[id] = old
			return Run{}, err
		}
		return Run{}, fmt.Errorf("goal expired")
	}
	if g.Runs >= g.MaxRuns || g.TokensUsed >= g.TokenBudget {
		old := g
		if g.Runs >= g.MaxRuns {
			g.StatusReason = "run_limit_exhausted"
		} else {
			g.StatusReason = "budget_exhausted"
		}
		g.Status = StatusPaused
		g.UpdatedAt = now.UTC()
		g.Revision++
		s.data.Goals[id] = g
		if err := s.saveLocked(); err != nil {
			s.data.Goals[id] = old
			return Run{}, err
		}
		return Run{}, fmt.Errorf("goal paused: %s", g.StatusReason)
	}
	for _, r := range s.data.Runs[id] {
		if r.FinishedAt == nil {
			return Run{}, fmt.Errorf("goal run already active")
		}
	}
	old := g
	oldRuns := cloneRuns(s.data.Runs[id])
	if g.Managed && g.ConfigRevision == 0 {
		// Upgrade legacy managed cycles before capturing the run snapshot. This
		// keeps subsequent checkpoint revisions from invalidating the run fence.
		g.ConfigRevision = 1
	}
	// Generation belongs to each run, not merely to the cycle.  This keeps a
	// later run from colliding with an intent produced by an earlier run.
	generation := int64(g.CycleSequence)
	for _, priorRun := range s.data.Runs[id] {
		if priorRun.Generation >= generation {
			generation = priorRun.Generation + 1
		}
	}
	if prior, ok := s.data.ScheduleIntents[intentKey(id, "goal")]; ok && prior.Generation >= generation {
		generation = prior.Generation + 1
	}
	run := Run{ID: uuid.NewString(), GoalID: id, SessionID: g.SessionID, Status: "running", Reason: reason, StartedAt: now.UTC(), GoalRevision: g.Revision, ProfileRevision: profile.Revision, ConfigRevision: g.ConfigRevision, Generation: generation}
	s.data.Runs[id] = append(s.data.Runs[id], run)
	g.Runs++
	next := now.Add(time.Duration(g.MinWakeIntervalSeconds) * time.Second)
	g.NextWakeAt = &next
	g.UpdatedAt = now.UTC()
	g.Revision++
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = old
		s.data.Runs[id] = oldRuns
		return Run{}, err
	}
	return run, nil
}
func (s *Store) FinishRun(id string, run Run, now time.Time) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	idx := -1
	for i := range s.data.Runs[id] {
		if s.data.Runs[id][i].ID == run.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Run{}, fmt.Errorf("run not found")
	}
	if run.Checkpoint == nil {
		run.Checkpoint = s.data.Runs[id][idx].Checkpoint
	}
	if s.data.Runs[id][idx].FinishedAt != nil {
		return s.data.Runs[id][idx], nil
	}
	if g.Status == StatusStopped || g.Status == StatusCompleted {
		return Run{}, ErrNotRunnable
	}
	oldg, oldr := g, s.data.Runs[id][idx]
	oldUsage := s.data.Usage[g.AgentID]
	oldReceipt, hadReceipt := s.data.UsageReceipts[run.ID]
	if run.TokensUsed < 0 {
		return Run{}, fmt.Errorf("negative run usage")
	}
	run.FinishedAt = &now
	if run.Status == "" {
		run.Status = "completed"
	}
	s.data.Runs[id][idx] = run
	if run.TokensUsed > 0 {
		if _, _, err := s.recordRunUsageLocked(g.AgentID, run.ID, run.TokensUsed, 0, 0, now); err != nil {
			s.data.Runs[id][idx] = oldr
			return Run{}, err
		}
	} else {
		u := oldUsage
		u.AgentID = g.AgentID
		u.Unknown = true
		u.UnknownReason = "usage reconciliation required"
		u.UpdatedAt = now.UTC()
		s.data.Usage[g.AgentID] = u
	}
	g.TokensUsed += run.TokensUsed
	if run.Checkpoint != nil {
		g.LastCheckpoint = cloneCheckpoint(run.Checkpoint)
		if run.Status == "completed" && checkpointProvesCompletion(run.Checkpoint) {
			g.Status = StatusCompleted
		} else if g.Status == StatusActive {
			g.Status = StatusWaiting
		}
	}
	g.UpdatedAt = now.UTC()
	g.Revision++
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = oldg
		s.data.Runs[id][idx] = oldr
		s.data.Usage[g.AgentID] = oldUsage
		if hadReceipt {
			s.data.UsageReceipts[run.ID] = oldReceipt
		} else {
			delete(s.data.UsageReceipts, run.ID)
		}
		return Run{}, err
	}
	return run, nil
}
func (s *Store) Runs(id string) []Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRuns(s.data.Runs[id])
}
func (s *Store) Checkpoint(goalID, runID string, cp Checkpoint, now time.Time) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.data.Goals[goalID]
	if !ok {
		return Run{}, ErrNotFound
	}
	if g.Status == StatusStopped || g.Status == StatusCompleted {
		return Run{}, ErrNotRunnable
	}
	var found *Run
	idx := -1
	for i := range s.data.Runs[goalID] {
		if s.data.Runs[goalID][i].ID == runID {
			found = &s.data.Runs[goalID][i]
			idx = i
			break
		}
	}
	if found == nil {
		return Run{}, fmt.Errorf("run not found")
	}
	oldGoal, oldRun := g, *found
	if found.FinishedAt != nil {
		return Run{}, ErrNotRunnable
	}
	cp.At = now.UTC()
	if cp.NextWakeAt != nil {
		min := now.Add(time.Duration(g.MinWakeIntervalSeconds) * time.Second)
		if cp.NextWakeAt.Before(min) {
			return Run{}, fmt.Errorf("next wake is below minimum interval")
		}
		if g.ExpiresAt != nil && !cp.NextWakeAt.Before(*g.ExpiresAt) {
			return Run{}, fmt.Errorf("next wake exceeds goal deadline")
		}
		g.NextWakeAt = cp.NextWakeAt
	}
	storedCheckpoint := cloneCheckpoint(&cp)
	found.Checkpoint = storedCheckpoint
	g.LastCheckpoint = cloneCheckpoint(storedCheckpoint)
	g.UpdatedAt = now.UTC()
	g.Revision++
	s.data.Goals[goalID] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[goalID] = oldGoal
		s.data.Runs[goalID][idx] = oldRun
		return Run{}, err
	}
	return cloneRun(*found), nil
}
