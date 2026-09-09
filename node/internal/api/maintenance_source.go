package api

import (
	"context"
	"fmt"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// maintenanceSource adapts the runtime SQLite store without making memory
// depend on the session persistence package.
type maintenanceSource struct {
	store *store.SQLiteStore
	goals *goals.Store
}

func (s maintenanceSource) LoadMaintenanceMessages(ctx context.Context, agentID string, after uint64, limit int) (memory.DurableMessageBatch, error) {
	if limit <= 0 {
		limit = 200
	}
	cursor := after
	skipped := false
	for page := 0; page < 16; page++ {
		if err := ctx.Err(); err != nil {
			return memory.DurableMessageBatch{}, err
		}
		items, err := s.store.ListCompletedTurnSnapshots(ctx, agentID, int64(cursor), limit)
		if err != nil {
			return memory.DurableMessageBatch{}, err
		}
		if len(items) == 0 {
			if skipped {
				return memory.DurableMessageBatch{Sequence: cursor, Complete: true, SkipOnly: true, SkippedThrough: cursor}, nil
			}
			return memory.DurableMessageBatch{}, nil
		}
		for _, item := range items {
			if s.goals != nil && s.goals.IsMaintenanceSession(agentID, item.SessionID) {
				cursor = uint64(item.EventID)
				skipped = true
				continue
			}
			if item.Status != "readable" {
				return memory.DurableMessageBatch{Sequence: uint64(item.EventID), Complete: false}, nil
			}
			return memory.DurableMessageBatch{SessionID: item.SessionID, Sequence: uint64(item.EventID), Messages: item.Messages, Complete: true}, nil
		}
	}
	if skipped {
		return memory.DurableMessageBatch{Sequence: cursor, Complete: true, SkipOnly: true, SkippedThrough: cursor}, nil
	}
	return memory.DurableMessageBatch{}, fmt.Errorf("maintenance source exceeded bounded skip pages")
}

var _ memory.DurableMaintenanceSource = maintenanceSource{}
