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
}

func (s *agentLongTermStore) ReadLongTerm(ctx context.Context) (turn.LongTermSnapshot, error) {
	if s == nil || s.agents == nil {
		return turn.LongTermSnapshot{}, nil
	}
	rec, err := s.agents.EnsureAgentPromptContext(ctx, s.agentID, s.runtimeDir)
	if err != nil {
		return turn.LongTermSnapshot{}, err
	}
	if rec == nil {
		return turn.LongTermSnapshot{}, nil
	}
	return turn.LongTermSnapshot{
		Content: rec.LongTermMD,
		Version: rec.UpdatedAt,
	}, nil
}

func (s *agentLongTermStore) SaveLongTerm(ctx context.Context, content string, expectedVersion time.Time) error {
	if s == nil || s.agents == nil {
		return nil
	}
	ok, err := s.agents.UpdateAgentLongTermCAS(ctx, s.agentID, content, expectedVersion)
	if err != nil {
		return err
	}
	if !ok {
		return turn.ErrLongTermVersionConflict
	}
	return nil
}

var _ turn.LongTermStore = (*agentLongTermStore)(nil)
