package api

import (
	"context"
	"fmt"
	"strings"
)

// softDeleteAgentCascade removes one ordinary Agent and its runtime. Browser
// is a session resource now and is not persisted as an Agent record.
func (s *Server) softDeleteAgentCascade(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	rec, err := s.agents.Get(ctx, id)
	if err != nil {
		return err
	}
	if rec == nil || rec.Archived {
		return fmt.Errorf("agent not found")
	}
	if err := s.agents.SoftDelete(ctx, id); err != nil {
		return err
	}
	if s.sessions != nil {
		_, _ = s.sessions.Delete(id)
	}
	return nil
}
