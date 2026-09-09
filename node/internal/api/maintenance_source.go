package api

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// maintenanceSource adapts the runtime SQLite store without making memory
// depend on the session persistence package.
type maintenanceSource struct{ store *store.SQLiteStore }

func (s maintenanceSource) LoadMaintenanceMessages(ctx context.Context, agentID string, after uint64, limit int) (memory.DurableMessageBatch, error) {
	items, err := s.store.ListCompletedTurnSnapshots(ctx, agentID, int64(after), limit)
	if err != nil || len(items) == 0 {
		return memory.DurableMessageBatch{}, err
	}
	item := items[0]
	if item.Status != "readable" {
		return memory.DurableMessageBatch{Sequence: uint64(item.EventID), Complete: false}, nil
	}
	return memory.DurableMessageBatch{SessionID: item.SessionID, Sequence: uint64(item.EventID), Messages: item.Messages, Complete: true}, nil
}

var _ memory.DurableMaintenanceSource = maintenanceSource{}
