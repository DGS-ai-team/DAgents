package goals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("goal not found")
var ErrNotRunnable = errors.New("goal is not runnable")

type disk struct {
	Goals map[string]Goal  `json:"goals"`
	Runs  map[string][]Run `json:"runs"`
}
type Store struct {
	mu   sync.RWMutex
	path string
	data disk
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
	s := &Store{path: path, data: disk{Goals: map[string]Goal{}, Runs: map[string][]Run{}}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("parse goals store: %w", err)
	}
	if s.data.Goals == nil {
		s.data.Goals = map[string]Goal{}
	}
	if s.data.Runs == nil {
		s.data.Runs = map[string][]Run{}
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

// UpdateAutonomyIntent atomically updates the user intent for an existing
// autonomy Goal. Budget, run counters and scheduling limits are immutable here.
func (s *Store) UpdateAutonomyIntent(id, objective, acceptance string, nextWakeAt *time.Time, now time.Time) (Goal, error) {
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
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = old
		return Goal{}, err
	}
	return g, nil
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
	run := Run{ID: uuid.NewString(), GoalID: id, SessionID: g.SessionID, Status: "running", Reason: reason, StartedAt: now.UTC()}
	s.data.Runs[id] = append(s.data.Runs[id], run)
	g.Runs++
	next := now.Add(time.Duration(g.MinWakeIntervalSeconds) * time.Second)
	g.NextWakeAt = &next
	g.UpdatedAt = now.UTC()
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
	if g.Status == StatusStopped || g.Status == StatusCompleted {
		return Run{}, ErrNotRunnable
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
	oldg, oldr := g, s.data.Runs[id][idx]
	run.FinishedAt = &now
	if run.Status == "" {
		run.Status = "completed"
	}
	s.data.Runs[id][idx] = run
	g.TokensUsed += run.TokensUsed
	if run.Checkpoint != nil {
		g.LastCheckpoint = run.Checkpoint
		if run.Status == "completed" && checkpointProvesCompletion(run.Checkpoint) {
			g.Status = StatusCompleted
		} else if g.Status == StatusActive {
			g.Status = StatusWaiting
		}
	}
	g.UpdatedAt = now.UTC()
	s.data.Goals[id] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[id] = oldg
		s.data.Runs[id][idx] = oldr
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
	found.Checkpoint = &cp
	g.LastCheckpoint = &cp
	g.UpdatedAt = now.UTC()
	s.data.Goals[goalID] = g
	if err := s.saveLocked(); err != nil {
		s.data.Goals[goalID] = oldGoal
		s.data.Runs[goalID][idx] = oldRun
		return Run{}, err
	}
	return cloneRun(*found), nil
}
