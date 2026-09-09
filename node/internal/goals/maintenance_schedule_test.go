package goals

import (
	"path/filepath"
	"testing"
	"time"
)

func saveDailyProfile(t *testing.T, s *Store, agent, zone, schedule string, enabled bool, now time.Time) AutoProfile {
	t.Helper()
	p, err := s.SaveProfile(AutoProfile{AgentID: agent, Enabled: enabled, MaintenanceEnabled: enabled, MaintenanceSchedule: schedule, Timezone: zone}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMaintenanceScheduleClaimPersistsAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goals.json")
	now := time.Date(2025, 4, 3, 10, 0, 0, 0, time.UTC)
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	p := saveDailyProfile(t, s, "daily-agent", "UTC", "daily 09:00", true, now.Add(-24*time.Hour))
	claim, err := s.ClaimMaintenance("daily-agent", now)
	if err != nil || !claim.Claimed || claim.Occurrence == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	if claim.Occurrence.LocalDate != "2025-04-03" || claim.Occurrence.ScheduleRevision != p.MaintenanceRevision {
		t.Fatalf("occurrence=%+v", claim.Occurrence)
	}
	second, err := s.ClaimMaintenance("daily-agent", now.Add(time.Minute))
	if err != nil || second.Claimed || second.RecoveryRequired || second.Occurrence == nil || second.Occurrence.Status != MaintenanceOccurrencePending {
		t.Fatalf("duplicate claim=%+v err=%v", second, err)
	}
	if _, err := s.FinishMaintenance("daily-agent", claim.Occurrence.LocalDate, p.MaintenanceRevision, MaintenanceOccurrenceCompleted, "ok", "", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	view, err := reopened.MaintenanceScheduleStatus("daily-agent", now)
	if err != nil || view.Last == nil || view.Last.Status != MaintenanceOccurrenceCompleted {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if view.NextAt == nil || !view.NextAt.Equal(time.Date(2025, 4, 4, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_at=%v", view.NextAt)
	}
	claimAgain, err := reopened.ClaimMaintenance("daily-agent", now.Add(2*time.Hour))
	if err != nil || claimAgain.Claimed {
		t.Fatalf("reopened duplicate=%+v err=%v", claimAgain, err)
	}
}

func TestMaintenanceScheduleDisabledDoesNotClaim(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2025, 4, 3, 10, 0, 0, 0, time.UTC)
	saveDailyProfile(t, s, "disabled-agent", "UTC", "daily 09:00", false, now)
	claim, err := s.ClaimMaintenance("disabled-agent", now)
	if err != nil || claim.Claimed || claim.RecoveryRequired {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	view, err := s.MaintenanceScheduleStatus("disabled-agent", now)
	if err != nil || view.Enabled || view.NextAt != nil {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}

func TestMaintenanceScheduleEpochDoesNotCatchUpBeforeEnable(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2025, 4, 3, 8, 0, 0, 0, time.UTC)
	saveDailyProfile(t, s, "epoch-agent", "UTC", "daily 09:00", true, now)
	claim, err := s.ClaimMaintenance("epoch-agent", now)
	if err != nil || claim.Claimed {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
}

func TestMaintenanceSchedulePendingLiveTickAndReopenRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goals.json")
	now := time.Date(2025, 4, 3, 10, 0, 0, 0, time.UTC)
	s, _ := OpenStore(path)
	p := saveDailyProfile(t, s, "pending-agent", "UTC", "daily 09:00", true, now.Add(-24*time.Hour))
	first, err := s.ClaimMaintenance("pending-agent", now)
	if err != nil || !first.Claimed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	live, err := s.ClaimMaintenance("pending-agent", now.Add(time.Minute))
	if err != nil || live.Claimed || live.RecoveryRequired || live.Occurrence == nil || live.Occurrence.Status != MaintenanceOccurrencePending {
		t.Fatalf("live=%+v err=%v", live, err)
	}
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.ClaimMaintenance("pending-agent", now.Add(2*time.Minute))
	if err != nil || recovered.Claimed || !recovered.RecoveryRequired || recovered.Occurrence == nil || recovered.Occurrence.Status != MaintenanceOccurrenceRecoveryRequired {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	if _, err := reopened.FinishMaintenance("pending-agent", recovered.Occurrence.LocalDate, p.MaintenanceRevision, MaintenanceOccurrenceFailed, "", "inspected", now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceScheduleFreshStorePendingIsNotRecovery(t *testing.T) {
	now := time.Date(2025, 4, 3, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "goals.json")
	s, _ := OpenStore(path)
	// Reopen an empty persisted store: there is no recovery set yet.
	s, _ = OpenStore(path)
	saveDailyProfile(t, s, "fresh-agent", "UTC", "daily 09:00", true, now.Add(-24*time.Hour))
	first, err := s.ClaimMaintenance("fresh-agent", now)
	if err != nil || !first.Claimed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	repeat, err := s.ClaimMaintenance("fresh-agent", now.Add(time.Minute))
	if err != nil || repeat.Claimed || repeat.RecoveryRequired || repeat.Occurrence == nil || repeat.Occurrence.Status != MaintenanceOccurrencePending {
		t.Fatalf("repeat=%+v err=%v", repeat, err)
	}
}

func TestMaintenanceScheduleRecoveryBlocksLaterDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goals.json")
	now := time.Date(2025, 4, 3, 10, 0, 0, 0, time.UTC)
	s, _ := OpenStore(path)
	saveDailyProfile(t, s, "blocked-agent", "UTC", "daily 09:00", true, now.Add(-24*time.Hour))
	first, _ := s.ClaimMaintenance("blocked-agent", now)
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := reopened.ClaimMaintenance("blocked-agent", now.Add(time.Minute))
	if err != nil || !recovery.RecoveryRequired {
		t.Fatalf("recovery=%+v err=%v", recovery, err)
	}
	later, err := reopened.ClaimMaintenance("blocked-agent", now.Add(24*time.Hour))
	if err != nil || later.Claimed || !later.RecoveryRequired || later.Occurrence == nil || later.Occurrence.LocalDate != first.Occurrence.LocalDate {
		t.Fatalf("later=%+v err=%v", later, err)
	}
}

func TestMaintenanceScheduleProfileRevisionDoesNotChangeOccurrenceRevision(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	now := time.Date(2025, 4, 3, 10, 0, 0, 0, time.UTC)
	p := saveDailyProfile(t, s, "revision-agent", "UTC", "daily 09:00", true, now.Add(-24*time.Hour))
	if _, err := s.ClaimMaintenance("revision-agent", now); err != nil {
		t.Fatal(err)
	}
	updated, err := s.SaveProfile(AutoProfile{AgentID: "revision-agent", Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC", MaintenanceTokenBudget: 99}, p.Revision, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated.MaintenanceRevision != p.MaintenanceRevision {
		t.Fatalf("maintenance revision changed: %d -> %d", p.MaintenanceRevision, updated.MaintenanceRevision)
	}
	view, err := s.MaintenanceScheduleStatus("revision-agent", now.Add(time.Hour))
	if err != nil || view.Last == nil {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if _, err := s.SaveProfile(AutoProfile{AgentID: "revision-agent", Enabled: true, MaintenanceEnabled: false, Timezone: "UTC"}, updated.Revision, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	disabled, err := s.MaintenanceScheduleStatus("revision-agent", now.Add(time.Hour))
	if err != nil || disabled.Enabled || disabled.Last == nil {
		t.Fatalf("disabled view=%+v err=%v", disabled, err)
	}
}

func TestMaintenanceScheduleEnableActionStartsNewEpoch(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	initial := time.Date(2025, 4, 3, 8, 0, 0, 0, time.UTC)
	p, err := s.SaveProfile(AutoProfile{AgentID: "action-agent", Enabled: false, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC"}, 0, initial)
	if err != nil {
		t.Fatal(err)
	}
	enabled, _, err := s.ApplyAutoAction("action-agent", "", "enable_auto", p.Revision, 0, initial.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if enabled.MaintenanceEpochAt.Equal(initial) || enabled.MaintenanceRevision <= p.MaintenanceRevision {
		t.Fatalf("enabled=%+v initial=%+v", enabled, p)
	}
	claim, err := s.ClaimMaintenance("action-agent", initial.Add(30*time.Minute))
	if err != nil || claim.Claimed {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
}

func TestMaintenanceScheduleStatusLastIsDeterministicAcrossRevisions(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	now := time.Date(2025, 4, 3, 10, 0, 0, 0, time.UTC)
	p := saveDailyProfile(t, s, "last-agent", "UTC", "daily 09:00", true, now.Add(-24*time.Hour))
	first, err := s.ClaimMaintenance("last-agent", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishMaintenance("last-agent", first.Occurrence.LocalDate, p.MaintenanceRevision, MaintenanceOccurrenceCompleted, "first", "", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	updated, err := s.SaveProfile(AutoProfile{AgentID: "last-agent", Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 11:00", Timezone: "UTC"}, p.Revision, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ClaimMaintenance("last-agent", now.Add(3*time.Hour))
	if err != nil || !second.Claimed {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	view, err := s.MaintenanceScheduleStatus("last-agent", now.Add(3*time.Hour))
	if err != nil || view.Last == nil || view.Last.ScheduleRevision != updated.MaintenanceRevision {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}

func TestMaintenanceScheduleDSTGapAndOverlapAreSingleDailyOccurrence(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	schedule, _ := ParseWorkSchedule("daily 02:30")
	gapDate := time.Date(2024, 3, 10, 0, 0, 0, 0, loc)
	gap := maintenanceOccurrenceForDate(gapDate, schedule, loc)
	gapLocal := gap.In(loc)
	if gapLocal.Year() != 2024 || gapLocal.Month() != 3 || gapLocal.Day() != 10 || gapLocal.Hour() != 3 || gapLocal.Minute() != 0 {
		t.Fatalf("gap resolved to %s", gapLocal)
	}
	overlapSchedule, _ := ParseWorkSchedule("daily 01:30")
	overlapDate := time.Date(2024, 11, 3, 0, 0, 0, 0, loc)
	first := maintenanceOccurrenceForDate(overlapDate, overlapSchedule, loc)
	second := maintenanceOccurrenceForDate(overlapDate, overlapSchedule, loc)
	if !first.Equal(second) || first.In(loc).Format("2006-01-02") != "2024-11-03" {
		t.Fatalf("overlap first=%s second=%s", first, second)
	}
}

func TestMaintenanceScheduleDSTOverlapClaimIsOneOccurrence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goals.json")
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := OpenStore(path)
	profile := saveDailyProfile(t, s, "overlap-agent", "America/New_York", "daily 01:30", true, time.Date(2024, 11, 2, 12, 0, 0, 0, loc).UTC())
	firstNow := time.Date(2024, 11, 3, 6, 40, 0, 0, time.UTC)
	first, err := s.ClaimMaintenance("overlap-agent", firstNow)
	if err != nil || !first.Claimed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if _, err := s.FinishMaintenance("overlap-agent", first.Occurrence.LocalDate, profile.MaintenanceRevision, MaintenanceOccurrenceCompleted, "ok", "", firstNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	second, err := s.ClaimMaintenance("overlap-agent", time.Date(2024, 11, 3, 7, 40, 0, 0, time.UTC))
	if err != nil || second.Claimed || second.Occurrence == nil || second.Occurrence.LocalDate != first.Occurrence.LocalDate {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}
