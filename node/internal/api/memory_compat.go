package api

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func (s *Server) openAgentMemoryService(id string, rec *store.AgentRecord) (*memory.LocalService, error) {
	if s == nil || s.cfg == nil || rec == nil {
		return nil, nil
	}
	snapshot, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return nil, err
	}
	// Automatic recall and model-facing memory tools are separate capabilities.
	// Open the v2 projection when either is enabled. Legacy Agents with neither
	// an explicit long_term_enabled flag nor the memory tool group keep using
	// the compatibility store and do not create a workspace memory database.
	memoryToolsOn := false
	for _, group := range agentruntime.EnabledToolGroups(snapshot) {
		if strings.EqualFold(strings.TrimSpace(group), "memory") {
			memoryToolsOn = true
			break
		}
	}
	memoryAutoRecall := false
	if promptCtx := agentruntime.PromptContextFromDefaults(snapshot); promptCtx != nil && promptCtx.LongTermEnabled != nil {
		memoryAutoRecall = *promptCtx.LongTermEnabled
	}
	if !memoryToolsOn && !memoryAutoRecall {
		return nil, nil
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
	if agentruntime.LongTermScopeFromDefaults(snapshot) == store.LongTermScopeGlobal {
		scope = memory.ScopeGlobal
	}
	return memory.OpenLocalService(
		filepath.Join(stateRoot, "memory", "memory.db"),
		filepath.Join(s.runtimeDir(), "memory", "global.db"),
		scope,
	)
}

func memoryEntriesToTurn(entries []memory.Entry) []turn.LongTermEntry {
	out := make([]turn.LongTermEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Content) == "" {
			continue
		}
		out = append(out, turn.LongTermEntry{ID: entry.ID, Content: entry.Content, CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt})
	}
	return out
}

func memoryEntriesToViews(entries []memory.Entry) []longTermEntryView {
	out := make([]longTermEntryView, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Content) == "" {
			continue
		}
		out = append(out, longTermEntryView{ID: entry.ID, Content: strings.TrimSpace(entry.Content), CreatedAt: formatLongTermDate(entry.CreatedAt), UpdatedAt: formatLongTermDate(entry.UpdatedAt)})
	}
	return out
}

func legacyViewsToMemoryEntries(views []longTermEntryView, scope memory.Scope) []memory.Entry {
	out := make([]memory.Entry, 0, len(views))
	for _, view := range views {
		content := strings.TrimSpace(view.Content)
		if content == "" {
			continue
		}
		entry := memory.Entry{ID: strings.TrimSpace(view.ID), Scope: scope, Tier: memory.TierRecall, Kind: memory.KindFact, Content: content, Status: memory.StatusActive, SourceType: "settings"}
		if view.CreatedAt != "" {
			entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, view.CreatedAt)
		}
		if view.UpdatedAt != "" {
			entry.UpdatedAt, _ = time.Parse(time.RFC3339Nano, view.UpdatedAt)
		}
		out = append(out, entry)
	}
	return out
}

// migrateLegacyMemory copies the old JSON/Markdown projection into the new
// workspace memory store exactly once per scope. It is intentionally kept at
// the API/runtime boundary: the memory package does not know about agents.db
// and therefore cannot accidentally reintroduce that dependency.
func migrateLegacyMemory(ctx context.Context, agents *store.AgentStore, runtimeDir, agentID string, service *memory.LocalService) error {
	if agents == nil || service == nil {
		return nil
	}
	for _, scope := range []string{store.LongTermScopeAgent, store.LongTermScopeGlobal} {
		legacy := &agentLongTermStore{
			agents: agents, agentID: agentID, runtimeDir: runtimeDir, scope: scope,
		}
		snapshot, err := legacy.ReadLongTerm(ctx)
		if err != nil {
			return err
		}
		entries := make([]memory.Entry, 0, len(snapshot.Entries))
		for _, item := range snapshot.Entries {
			content := strings.TrimSpace(item.Content)
			if content == "" {
				continue
			}
			entries = append(entries, memory.Entry{
				ID: item.ID, Scope: memory.Scope(scope), Tier: memory.TierRecall,
				Kind: memory.KindFact, Content: content, Status: memory.StatusActive,
				SourceType: "legacy_long_term", CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
			})
		}
		if _, err := service.ImportLegacy(ctx, memory.Scope(scope), entries, memory.DigestEntries(entries)); err != nil {
			return err
		}
	}
	return nil
}
