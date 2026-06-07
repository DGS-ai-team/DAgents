package triggers

import (
	"log/slog"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

func discardLogger(l *slog.Logger) *slog.Logger {
	if l == nil {
		return logx.Discard()
	}
	return l
}

func definitionLogAttrs(def Definition) []any {
	attrs := []any{
		"trigger_id", def.TriggerID,
		"name", def.Name,
		"enabled", def.Enabled,
	}
	if kind, err := InferScheduleKind(def.Condition); err == nil {
		attrs = append(attrs, "schedule_kind", string(kind))
	}
	if def.NextFireAt != nil {
		attrs = append(attrs, "next_fire_at", *def.NextFireAt)
	}
	if def.TargetSessionID != nil && *def.TargetSessionID != "" {
		attrs = append(attrs, "target_session_id", *def.TargetSessionID)
	}
	return attrs
}

func fireRecordLogAttrs(record FireRecord) []any {
	attrs := []any{
		"trigger_id", record.TriggerID,
		"fire_id", record.FireID,
		"status", string(record.Status),
		"reason", record.Reason,
	}
	if record.Message != "" {
		attrs = append(attrs, "message", record.Message)
	}
	if record.SessionID != nil && *record.SessionID != "" {
		attrs = append(attrs, "session_id", *record.SessionID)
	}
	if record.ClientID != nil && *record.ClientID != "" {
		attrs = append(attrs, "client_id", *record.ClientID)
	}
	if record.Content != "" {
		attrs = append(attrs, "content_len", len(record.Content))
	}
	return attrs
}

func (s *Store) logCreated(def Definition) {
	s.logger.Info("trigger created", definitionLogAttrs(def)...)
}

func (s *Store) logUpdated(def Definition) {
	s.logger.Info("trigger updated", definitionLogAttrs(def)...)
}

func (s *Store) logDeleted(triggerID string) {
	s.logger.Info("trigger deleted", "trigger_id", triggerID)
}

func (s *Scheduler) logFireRecord(record FireRecord) {
	switch record.Status {
	case FireStatusQueued:
		s.logger.Info("trigger fired", fireRecordLogAttrs(record)...)
	case FireStatusSkipped:
		s.logger.Warn("trigger fire skipped", fireRecordLogAttrs(record)...)
	case FireStatusError:
		s.logger.Warn("trigger fire failed", fireRecordLogAttrs(record)...)
	default:
		s.logger.Info("trigger fire", fireRecordLogAttrs(record)...)
	}
}
