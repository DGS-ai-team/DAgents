package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// syncBrowserCompanion 按父 Agent 快照确保伴生存在或移除。
// 伴生方案：enabled_groups 含 browser 时创建/更新 companion；否则归档伴生。
func (s *Server) syncBrowserCompanion(ctx context.Context, parent store.AgentRecord) error {
	if s == nil || s.agents == nil {
		return nil
	}
	parentID := strings.TrimSpace(parent.AgentID)
	if parentID == "" || agentruntime.IsCompanionBrowserAgentID(parentID) {
		return nil
	}
	if agentruntime.IsBrowserCompanionRecord(parent.ConfigSnapshot) {
		return nil
	}

	snap, err := agentruntime.ParseSnapshot(parent.ConfigSnapshot)
	if err != nil {
		return fmt.Errorf("parse parent snapshot: %w", err)
	}
	companionID := agentruntime.CompanionBrowserAgentID(parentID)
	if !agentruntime.SnapshotHasBrowserGroup(snap) {
		return s.removeBrowserCompanion(ctx, companionID)
	}

	parentSnap, err := agentruntime.WithCompanionMeta(parent.ConfigSnapshot, agentruntime.CompanionMeta{
		BrowserAgentID: companionID,
	})
	if err != nil {
		return err
	}
	if string(parentSnap) != string(parent.ConfigSnapshot) {
		parent.ConfigSnapshot = parentSnap
		parent.UpdatedAt = time.Now().UTC()
		if err := s.agents.Save(ctx, parent); err != nil {
			return err
		}
	}

	existing, err := s.agents.Get(ctx, companionID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	compDefaults := map[string]any{
		"agent": map[string]any{
			"role":        "browser_companion",
			"description": "Browser companion for " + strings.TrimSpace(parent.DisplayName),
		},
		"llm": map[string]any{
			"max_tool_loops": agentruntime.DefaultMaxToolLoops,
		},
		// 方案 A：Chrome 由 sidecar browser_use.Agent 驱动；伴生不挂细粒度工具给 LLM。
		"tools": map[string]any{
			"enabled_groups": []any{},
		},
	}
	compSnap, err := marshalAgentSnapshot(strings.TrimSpace(parent.TemplateID), compDefaults)
	if err != nil {
		return err
	}
	compSnap, err = agentruntime.WithCompanionMeta(compSnap, agentruntime.CompanionMeta{
		Role:         "browser",
		OwnerAgentID: parentID,
	})
	if err != nil {
		return err
	}

	name := strings.TrimSpace(parent.DisplayName)
	if name == "" {
		name = parentID
	}
	name = name + " · Browser"

	if existing != nil && !existing.Archived {
		existing.DisplayName = name
		existing.ConfigSnapshot = compSnap
		existing.SandboxEnabled = false
		existing.SandboxBackend = "process"
		existing.UpdatedAt = now
		if err := s.agents.Save(ctx, *existing); err != nil {
			return err
		}
		return nil
	}

	rec := store.AgentRecord{
		AgentID:        companionID,
		DisplayName:    name,
		TemplateID:     strings.TrimSpace(parent.TemplateID),
		Origin:         store.AgentOriginLocal,
		SandboxEnabled: false,
		SandboxBackend: "process",
		ConfigSnapshot: compSnap,
		HostJSON:       encodeJSONRaw(localHostPayload()),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.agents.Save(ctx, rec); err != nil {
		return err
	}
	if err := s.ensureAgentWorkspace(companionID); err != nil && s.logger != nil {
		s.logger.Warn("browser companion workspace failed", "agent_id", companionID, "error", err)
	}
	return nil
}

func (s *Server) removeBrowserCompanion(ctx context.Context, companionID string) error {
	companionID = strings.TrimSpace(companionID)
	if companionID == "" || s.agents == nil {
		return nil
	}
	rec, err := s.agents.Get(ctx, companionID)
	if err != nil {
		return err
	}
	if rec == nil || rec.Archived {
		return nil
	}
	if s.browserMgr != nil {
		_, _ = s.browserMgr.Stop(ctx, companionID)
	}
	if err := s.agents.SoftDelete(ctx, companionID); err != nil {
		return err
	}
	if s.sessions != nil {
		_, _ = s.sessions.Delete(companionID)
	}
	return nil
}

// softDeleteAgentCascade 软删主 Agent 并级联其 browser 伴生。
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
	if agentruntime.IsBrowserCompanionRecord(rec.ConfigSnapshot) || agentruntime.IsCompanionBrowserAgentID(id) {
		return fmt.Errorf("browser companion cannot be deleted directly; delete the owner agent")
	}
	meta := agentruntime.ParseCompanionMeta(rec.ConfigSnapshot)
	companionID := strings.TrimSpace(meta.BrowserAgentID)
	if companionID == "" {
		companionID = agentruntime.CompanionBrowserAgentID(id)
	}
	_ = s.removeBrowserCompanion(ctx, companionID)
	if err := s.agents.SoftDelete(ctx, id); err != nil {
		return err
	}
	if s.sessions != nil {
		_, _ = s.sessions.Delete(id)
	}
	return nil
}

func isHiddenCompanionAgent(rec store.AgentRecord) bool {
	if agentruntime.IsCompanionBrowserAgentID(rec.AgentID) {
		return true
	}
	return agentruntime.IsBrowserCompanionRecord(rec.ConfigSnapshot)
}

// companionMetaJSON 供测试/调试序列化。
func companionMetaJSON(meta agentruntime.CompanionMeta) json.RawMessage {
	raw, _ := json.Marshal(meta)
	return raw
}
