package api

import (
	"path/filepath"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// openAgentMemoryService opens the single workspace-backed memory service for
// an Agent. Capability flags control recall and tool exposure; they do not
// select a different persistence implementation.
func (s *Server) openAgentMemoryService(id string, rec *store.AgentRecord) (*memory.LocalService, error) {
	if s == nil || s.cfg == nil || rec == nil {
		return nil, nil
	}
	snapshot, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return nil, err
	}
	workspaceRoot, err := agentruntime.EnsureWorkspace(s.runtimeDir(), id, snapshot.Workspace)
	if err != nil {
		return nil, err
	}
	stateRoot, err := agentruntime.EnsureWorkspaceState(workspaceRoot, id)
	if err != nil {
		return nil, err
	}
	scope := memory.ScopeAgent
	if agentruntime.MemoryScopeFromDefaults(snapshot) == string(memory.ScopeGlobal) {
		scope = memory.ScopeGlobal
	}
	return memory.OpenLocalService(
		filepath.Join(stateRoot, "memory", "memory.db"),
		filepath.Join(s.runtimeDir(), "memory", "global.db"),
		scope,
	)
}
