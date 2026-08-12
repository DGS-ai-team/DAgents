package api

import (
	"context"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type agentLongTermStore struct {
	agents     *store.AgentStore
	agentID    string
	runtimeDir string
	scope      string
}

func (s *agentLongTermStore) ReadLongTerm(ctx context.Context) (turn.LongTermSnapshot, error) {
	if s == nil || s.agents == nil {
		return turn.LongTermSnapshot{}, nil
	}
	scope := normalizeAPIScope(s.scope)
	legacyMD := ""
	if scope == store.LongTermScopeAgent {
		if pc, err := s.agents.EnsureAgentPromptContext(ctx, s.agentID, s.runtimeDir); err == nil && pc != nil {
			legacyMD = pc.LongTermMD
		}
	}
	rec, err := s.agents.EnsureLongTermRecord(ctx, scope, s.agentID, s.runtimeDir, legacyMD)
	if err != nil {
		return turn.LongTermSnapshot{}, err
	}
	if rec == nil {
		return turn.LongTermSnapshot{Scope: scope}, nil
	}
	return turn.LongTermSnapshot{
		Scope:   rec.Scope,
		Entries: storeEntriesToTurn(rec.Entries),
		Version: rec.UpdatedAt,
	}, nil
}

func (s *agentLongTermStore) SaveLongTerm(ctx context.Context, entries []turn.LongTermEntry, expectedVersion time.Time) error {
	if s == nil || s.agents == nil {
		return nil
	}
	scope := normalizeAPIScope(s.scope)
	rec := store.LongTermRecord{
		Scope:   scope,
		AgentID: s.agentID,
		Entries: turnEntriesToStore(entries),
	}
	ok, err := s.agents.SaveLongTermRecordCAS(ctx, rec, expectedVersion)
	if err != nil {
		return err
	}
	if !ok {
		return turn.ErrLongTermVersionConflict
	}
	return nil
}

func normalizeAPIScope(scope string) string {
	if scope == turn.LongTermScopeGlobal {
		return store.LongTermScopeGlobal
	}
	return store.LongTermScopeAgent
}

func storeEntriesToTurn(entries []store.LongTermEntry) []turn.LongTermEntry {
	out := make([]turn.LongTermEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, turn.LongTermEntry{
			ID:        e.ID,
			Content:   e.Content,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		})
	}
	return out
}

func turnEntriesToStore(entries []turn.LongTermEntry) []store.LongTermEntry {
	out := make([]store.LongTermEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, store.LongTermEntry{
			ID:        e.ID,
			Content:   e.Content,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		})
	}
	return out
}

var _ turn.LongTermStore = (*agentLongTermStore)(nil)
