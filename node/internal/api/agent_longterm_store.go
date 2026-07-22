package api

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type agentLongTermStore struct {
	agents     *store.AgentStore
	agentID    string
	runtimeDir string
}

func (s *agentLongTermStore) ReadLongTerm(ctx context.Context) (string, error) {
	if s == nil || s.agents == nil {
		return "", nil
	}
	rec, err := s.agents.EnsureAgentPromptContext(ctx, s.agentID, s.runtimeDir)
	if err != nil {
		return "", err
	}
	if rec == nil {
		return "", nil
	}
	return rec.LongTermMD, nil
}

func (s *agentLongTermStore) SaveLongTerm(ctx context.Context, content string) error {
	if s == nil || s.agents == nil {
		return nil
	}
	rec, err := s.agents.EnsureAgentPromptContext(ctx, s.agentID, s.runtimeDir)
	if err != nil {
		return err
	}
	if rec == nil {
		rec = &store.AgentPromptContextRecord{AgentID: s.agentID}
	}
	rec.LongTermMD = content
	return s.agents.SaveAgentPromptContext(ctx, *rec)
}

var _ turn.LongTermStore = (*agentLongTermStore)(nil)
