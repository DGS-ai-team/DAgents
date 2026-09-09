package goals

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MaintenanceOccurrence is the durable coordination record for one local
// calendar day. It is deliberately separate from MaintenanceReceipt, which
// accounts for the LLM operation after the controller has claimed work.
type MaintenanceOccurrence struct {
	AgentID          string     `json:"agent_id"`
	LocalDate        string     `json:"local_date"`
	ScheduleRevision int64      `json:"schedule_revision"`
	ScheduledAt      time.Time  `json:"scheduled_at"`
	Status           string     `json:"status"`
	ClaimedAt        time.Time  `json:"claimed_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	Result           string     `json:"result,omitempty"`
	Error            string     `json:"error,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
	RecoveryCount    int        `json:"recovery_count,omitempty"`
	LastRecoveryAt   *time.Time `json:"last_recovery_at,omitempty"`
}

const (
	MaintenanceOccurrencePending          = "pending"
	MaintenanceOccurrenceCompleted        = "completed"
	MaintenanceOccurrenceFailed           = "failed"
	MaintenanceOccurrenceRecoveryRequired = "recovery_required"
)

type MaintenanceClaim struct {
	Claimed          bool                   `json:"claimed"`
	RecoveryRequired bool                   `json:"recovery_required"`
	Occurrence       *MaintenanceOccurrence `json:"occurrence,omitempty"`
}

// MaintenanceScheduleView is the controller-facing snapshot. NextAt is
// always a future instant in the profile timezone represented as an instant.
type MaintenanceScheduleView struct {
	Enabled  bool                   `json:"enabled"`
	Schedule string                 `json:"schedule"`
	Timezone string                 `json:"timezone"`
	NextAt   *time.Time             `json:"next_at,omitempty"`
	Last     *MaintenanceOccurrence `json:"last,omitempty"`
}

func maintenanceOccurrenceKey(agent, date string, revision int64) string {
	return strings.TrimSpace(agent) + "\x00" + date + "\x00" + strconv.FormatInt(revision, 10)
}

func maintenanceSchedule(profile AutoProfile) (*WorkSchedule, *time.Location, error) {
	if !profile.Enabled || !profile.MaintenanceEnabled || strings.TrimSpace(profile.MaintenanceSchedule) == "" {
		return nil, nil, nil
	}
	schedule, err := ParseWorkSchedule(profile.MaintenanceSchedule)
	if err != nil || schedule == nil || schedule.Kind != "daily" {
		return nil, nil, fmt.Errorf("invalid maintenance schedule: daily HH:MM required")
	}
	loc, err := LoadScheduleLocation(profile.Timezone)
	if err != nil {
		return nil, nil, err
	}
	return schedule, loc, nil
}

func maintenanceOccurrenceForDate(date time.Time, schedule *WorkSchedule, loc *time.Location) time.Time {
	return scheduleNextOnDate(date, schedule.Hour, schedule.Minute, loc)
}

// scheduleNextOnDate uses the existing calendar DST scanner. In a spring
// gap it returns the first valid instant after the requested wall clock; in a
// fall overlap the date key below ensures the wall time is claimed once.
func scheduleNextOnDate(date time.Time, hour, minute int, loc *time.Location) time.Time {
	return firstValidOnDate(date.In(loc), loc, hour, minute).UTC()
}

func (s *Store) ClaimMaintenance(agentID string, now time.Time) (MaintenanceClaim, error) {
	if s == nil || strings.TrimSpace(agentID) == "" || now.IsZero() {
		return MaintenanceClaim{}, fmt.Errorf("invalid maintenance claim")
	}
	agentID = strings.TrimSpace(agentID)
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, ok := s.data.Profiles[strings.TrimSpace(agentID)]
	if !ok {
		return MaintenanceClaim{}, ErrNotFound
	}
	schedule, loc, err := maintenanceSchedule(profile)
	if err != nil {
		return MaintenanceClaim{}, err
	}
	if schedule == nil {
		return MaintenanceClaim{}, nil
	}
	localNow := now.In(loc)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	candidate := maintenanceOccurrenceForDate(today, schedule, loc)
	dueDate := today
	if candidate.After(now) {
		dueDate = today.AddDate(0, 0, -1)
		candidate = maintenanceOccurrenceForDate(dueDate, schedule, loc)
	}
	dateKey := dueDate.Format("2006-01-02")
	revision := profile.MaintenanceRevision
	if revision <= 0 {
		revision = profile.Revision
	}
	key := maintenanceOccurrenceKey(agentID, dateKey, revision)
	for pendingKey, existing := range s.data.MaintenanceOccurrences {
		if existing.AgentID == agentID && (existing.Status == MaintenanceOccurrencePending || existing.Status == MaintenanceOccurrenceRecoveryRequired) {
			copy := existing
			if existing.Status == MaintenanceOccurrenceRecoveryRequired || s.recoveryOccurrences[pendingKey] {
				old := existing
				existing.Status = MaintenanceOccurrenceRecoveryRequired
				existing.Result = "recovery required before retry"
				existing.UpdatedAt = now
				s.data.MaintenanceOccurrences[pendingKey] = existing
				if err := s.saveLocked(); err != nil {
					s.data.MaintenanceOccurrences[pendingKey] = old
					return MaintenanceClaim{}, err
				}
				copy = existing
			}
			return MaintenanceClaim{RecoveryRequired: copy.Status == MaintenanceOccurrenceRecoveryRequired, Occurrence: &copy}, nil
		}
	}
	if existing, exists := s.data.MaintenanceOccurrences[key]; exists {
		copy := existing
		return MaintenanceClaim{Occurrence: &copy}, nil
	}
	if !profile.MaintenanceEpochAt.IsZero() && candidate.Before(profile.MaintenanceEpochAt) {
		return MaintenanceClaim{}, nil
	}
	occur := MaintenanceOccurrence{AgentID: agentID, LocalDate: dateKey, ScheduleRevision: revision, ScheduledAt: candidate, Status: MaintenanceOccurrencePending, ClaimedAt: now, UpdatedAt: now}
	old := cloneMaintenanceOccurrences(s.data.MaintenanceOccurrences)
	s.data.MaintenanceOccurrences[key] = occur
	if err := s.saveLocked(); err != nil {
		s.data.MaintenanceOccurrences = old
		return MaintenanceClaim{}, err
	}
	return MaintenanceClaim{Claimed: true, Occurrence: &occur}, nil
}

func cloneMaintenanceOccurrences(in map[string]MaintenanceOccurrence) map[string]MaintenanceOccurrence {
	out := make(map[string]MaintenanceOccurrence, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// FinishMaintenance closes a previously claimed occurrence. The identity is
// CAS-like: a caller cannot finish another agent, date, or schedule revision.
func (s *Store) FinishMaintenance(agentID, localDate string, scheduleRevision int64, status, result, reason string, now time.Time) (MaintenanceOccurrence, error) {
	if s == nil || strings.TrimSpace(agentID) == "" || localDate == "" || scheduleRevision <= 0 || now.IsZero() {
		return MaintenanceOccurrence{}, fmt.Errorf("invalid maintenance finish")
	}
	if status != MaintenanceOccurrenceCompleted && status != MaintenanceOccurrenceFailed && status != MaintenanceOccurrenceRecoveryRequired {
		return MaintenanceOccurrence{}, fmt.Errorf("invalid maintenance occurrence status")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := maintenanceOccurrenceKey(agentID, localDate, scheduleRevision)
	occur, ok := s.data.MaintenanceOccurrences[key]
	if !ok {
		return MaintenanceOccurrence{}, ErrNotFound
	}
	if occur.Status != MaintenanceOccurrencePending && occur.Status != MaintenanceOccurrenceRecoveryRequired {
		return MaintenanceOccurrence{}, ErrConflict
	}
	old := occur
	now = now.UTC()
	occur.Status, occur.Result, occur.Error, occur.UpdatedAt = status, result, reason, now
	occur.FinishedAt = &now
	s.data.MaintenanceOccurrences[key] = occur
	if err := s.saveLocked(); err != nil {
		s.data.MaintenanceOccurrences[key] = old
		return MaintenanceOccurrence{}, err
	}
	return occur, nil
}

func (s *Store) MaintenanceScheduleStatus(agentID string, now time.Time) (MaintenanceScheduleView, error) {
	if s == nil || strings.TrimSpace(agentID) == "" || now.IsZero() {
		return MaintenanceScheduleView{}, fmt.Errorf("invalid maintenance status")
	}
	now = now.UTC()
	agentID = strings.TrimSpace(agentID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.data.Profiles[strings.TrimSpace(agentID)]
	if !ok {
		return MaintenanceScheduleView{}, ErrNotFound
	}
	view := MaintenanceScheduleView{Enabled: profile.Enabled && profile.MaintenanceEnabled, Schedule: profile.MaintenanceSchedule, Timezone: profile.Timezone}
	var last *MaintenanceOccurrence
	for _, occurrence := range s.data.MaintenanceOccurrences {
		if occurrence.AgentID != agentID {
			continue
		}
		copy := occurrence
		if last == nil || newerMaintenanceOccurrence(copy, *last) {
			last = &copy
		}
	}
	view.Last = last
	schedule, loc, err := maintenanceSchedule(profile)
	if err != nil {
		return MaintenanceScheduleView{}, err
	}
	if schedule == nil {
		return view, nil
	}
	next, found := schedule.NextOccurrence(now, loc)
	if found {
		view.NextAt = &next
	}
	return view, nil
}

func newerMaintenanceOccurrence(a, b MaintenanceOccurrence) bool {
	if a.LocalDate != b.LocalDate {
		return a.LocalDate > b.LocalDate
	}
	if !a.ScheduledAt.Equal(b.ScheduledAt) {
		return a.ScheduledAt.After(b.ScheduledAt)
	}
	if !a.ClaimedAt.Equal(b.ClaimedAt) {
		return a.ClaimedAt.After(b.ClaimedAt)
	}
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.After(b.UpdatedAt)
	}
	return a.ScheduleRevision > b.ScheduleRevision
}
