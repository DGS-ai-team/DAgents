package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

// maintain applies only deterministic, local housekeeping. Expiry remains a
// query-time rule; the report counts expired rows but does not delete their
// audit history. Core overflow is demoted to Recall with a revision record.
func (s *Store) maintain(ctx context.Context, coreBudget int) (MaintenanceReport, error) {
	if coreBudget <= 0 {
		coreBudget = defaultCoreBudget
	}
	report := MaintenanceReport{Scope: s.scope, CoreBudget: coreBudget}
	now := time.Now().UTC()
	nowRaw := now.Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM memory_entries
WHERE scope=? AND status IN (?, ?) AND expires_at IS NOT NULL AND expires_at <> '' AND expires_at <= ?`,
		string(s.scope), string(StatusActive), string(StatusConflicted), nowRaw).Scan(&report.ExpiredEntries); err != nil {
		return report, err
	}
	entries, err := s.listWithQueryer(ctx, tx, `scope=? AND status=? AND tier=? AND
(expires_at IS NULL OR expires_at = '' OR expires_at > ?)
ORDER BY importance DESC, confidence DESC, updated_at DESC, id ASC`,
		string(s.scope), string(StatusActive), string(TierCore), nowRaw)
	if err != nil {
		return report, err
	}
	used := 0
	for _, entry := range entries {
		cost := tokens.EstimateInt(entry.Content) + tokens.EstimateInt(entry.ID) + 12
		if used+cost <= coreBudget {
			used += cost
			continue
		}
		updated := entry
		updated.Tier = TierRecall
		updated.Revision++
		updated.UpdatedAt = now
		result, updateErr := tx.ExecContext(ctx, `UPDATE memory_entries
SET tier=?, revision=?, updated_at=? WHERE id=? AND scope=? AND revision=?`,
			string(TierRecall), updated.Revision, nowRaw, entry.ID, string(s.scope), entry.Revision)
		if updateErr != nil {
			return report, updateErr
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return report, fmt.Errorf("memory entry revision conflict while demoting core %q", entry.ID)
		}
		if err := deleteFTSTx(ctx, tx, entry.ID, s.ftsAvailable()); err != nil {
			return report, err
		}
		if err := insertFTSTx(ctx, tx, updated, s.ftsAvailable()); err != nil {
			return report, err
		}
		if err := insertRevisionTx(ctx, tx, updated, "core_budget_demote"); err != nil {
			return report, err
		}
		report.DemotedCore++
		report.Changed = true
	}
	if !report.Changed {
		if err := tx.Commit(); err != nil {
			return report, err
		}
		return report, nil
	}
	revision, err := s.nextRevision(ctx, tx)
	if err != nil {
		return report, err
	}
	report.StoreRevision = revision
	if err := tx.Commit(); err != nil {
		return report, err
	}
	return report, nil
}
