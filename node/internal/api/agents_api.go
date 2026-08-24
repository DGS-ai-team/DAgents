package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/agenttemplate"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// errRemoteAgentNotLocal 已删除：遗留 remote stub 在 ensure 时归档并按 agent_not_found 处理。

func (s *Server) archiveRetiredRemoteStub(ctx context.Context, agentID string) {
	id := strings.TrimSpace(agentID)
	if s.agents == nil || id == "" {
		return
	}
	_ = s.agents.SoftDelete(ctx, id)
	if s.sessions != nil {
		_, _ = s.sessions.Delete(id)
	}
}

func (s *Server) writeAgentNotFound(w http.ResponseWriter, agentID string) {
	writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在",
		map[string]any{"agent_id": strings.TrimSpace(agentID)})
}

// retireRemoteStubIfNeeded 归档遗留 Placement remote 引用并写 404；返回 true 表示调用方应停止。
func (s *Server) retireRemoteStubIfNeeded(ctx context.Context, w http.ResponseWriter, rec *store.AgentRecord) bool {
	if rec == nil || store.NormalizeAgentOrigin(rec.Origin) != store.AgentOriginRemote {
		return false
	}
	s.archiveRetiredRemoteStub(ctx, rec.AgentID)
	s.writeAgentNotFound(w, rec.AgentID)
	return true
}

func (s *Server) registerAgentRoutes() {
	s.registerAgentTemplateRoutes()
	s.mux.HandleFunc("POST /v1/agents", s.handleCreateAgent)
	s.mux.HandleFunc("GET /v1/agents", s.handleListAgents)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}", s.handleGetAgent)
	s.mux.HandleFunc("PATCH /v1/agents/{agent_id}", s.handlePatchAgent)
	s.mux.HandleFunc("DELETE /v1/agents/{agent_id}", s.handleDeleteAgent)
	// Phase 2–4：agent 路径别名（内部仍走 session 实现，id 相同）。
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/ensure", s.handleAgentEnsure)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/reload", s.handleAgentReload)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/hydrate", s.handleAgentHydrate)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/execution-events", s.handleAgentExecutionEvents)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/timeline", s.handleAgentTimeline)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/turns/{turn_id}/steps/{step_id}/tool-executions/{execution_id}/reconcile", s.handleAgentReconcileToolExecution)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/cancel", s.handleAgentCancel)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/context", s.handleAgentContext)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/ack", s.handleAgentAck)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/clear-context", s.handleAgentClearContext)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/compress", s.handleAgentCompress)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/skills", s.handleAgentListSkills)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/skills/load", s.handleAgentLoadSkill)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/skills/unload", s.handleAgentUnloadSkill)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/child-agents", s.handleAgentListChildAgents)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/child-agents/{child_agent_id}", s.handleAgentGetChildAgent)
	s.mux.HandleFunc("POST /v1/agents/{agent_id}/child-agents/{child_agent_id}/cancel", s.handleAgentCancelChildAgent)
	s.registerAgentPolicyRoutes()
}

func (s *Server) templateLoader() *agenttemplate.Loader {
	builtin := agenttemplate.ResolveBuiltinDir()
	userDir := ""
	if s.cfg != nil {
		userDir = s.cfg.AgentTemplatesDir()
	}
	return agenttemplate.NewLoader(builtin, userDir)
}

// applyPromptContextBodies 在 Ensure 之后写入模板/请求带来的 soul/custom 预设。
func (s *Server) applyPromptContextBodies(ctx context.Context, agentID, soulMD, customMD string) error {
	if s.agents == nil {
		return fmt.Errorf("agents store unavailable")
	}
	pc, err := s.agents.GetAgentPromptContext(ctx, agentID)
	if err != nil {
		return err
	}
	if pc == nil {
		pc = &store.AgentPromptContextRecord{AgentID: agentID}
	}
	if strings.TrimSpace(soulMD) != "" {
		pc.SoulMD = strings.TrimSpace(soulMD)
	}
	if strings.TrimSpace(customMD) != "" {
		pc.CustomMD = strings.TrimSpace(customMD)
	}
	return s.agents.SaveAgentPromptContext(ctx, *pc)
}

func defaultAgentCreationDefaults() map[string]any {
	return map[string]any{
		"agent": map[string]any{
			"role":        "assistant",
			"description": "",
		},
		"llm": map[string]any{
			"max_tool_loops": agentruntime.DefaultMaxToolLoops,
		},
		"tools": map[string]any{
			"enabled_groups": []any{},
		},
		"skills": map[string]any{},
		"prompt_context": map[string]any{
			"soul_enabled":      true,
			"custom_enabled":    true,
			"long_term_enabled": true,
		},
	}
}

type createAgentRequest struct {
	// TemplateID 仅作溯源（可选）；配置由前端展开后通过 defaults 完整提交。
	TemplateID  string         `json:"template_id"`
	DisplayName string         `json:"display_name"`
	Origin      string         `json:"origin"` // 仅允许 local（或缺省）；remote 拒绝
	Defaults    map[string]any `json:"defaults"`
	// Placement 旧 Placement 请求字段；非本机 home_node_id 一律拒绝。
	Placement *struct {
		HomeNodeID string `json:"home_node_id"`
	} `json:"placement"`
}

type agentView struct {
	AgentID        string          `json:"agent_id"`
	DisplayName    string          `json:"display_name"`
	TemplateID     string          `json:"template_id"`
	Origin         string          `json:"origin"`
	ConfigSnapshot json.RawMessage `json:"config_snapshot,omitempty"`
	Placement      json.RawMessage `json:"placement,omitempty"`
	Host           json.RawMessage `json:"host,omitempty"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	LastActiveAt   string          `json:"last_active_at,omitempty"`
	// 以下字段供托盘 / 通知同步（agent_id 与内部 session 1:1）。
	Active           bool   `json:"active,omitempty"`
	HasActiveTurn    bool   `json:"has_active_turn,omitempty"`
	RunTurnPhase     string `json:"run_turn_phase,omitempty"`
	NotifySeq        int    `json:"notify_seq,omitempty"`
	AckSeq           int    `json:"ack_seq,omitempty"`
	HasUnread        bool   `json:"has_unread,omitempty"`
	HasPendingHITL   bool   `json:"has_pending_hitl,omitempty"`
	PendingHITLItems int    `json:"pending_hitl_items,omitempty"`
}

func agentViewFromRecord(rec store.AgentRecord) agentView {
	v := agentView{
		AgentID:        rec.AgentID,
		DisplayName:    rec.DisplayName,
		TemplateID:     rec.TemplateID,
		Origin:         store.NormalizeAgentOrigin(rec.Origin),
		ConfigSnapshot: rec.ConfigSnapshot,
		CreatedAt:      rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if len(rec.PlacementJSON) > 0 && string(rec.PlacementJSON) != "{}" {
		v.Placement = rec.PlacementJSON
	}
	if len(rec.HostJSON) > 0 && string(rec.HostJSON) != "{}" {
		v.Host = rec.HostJSON
	}
	return v
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	var req createAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}

	homeNodeID := ""
	if req.Placement != nil {
		homeNodeID = strings.TrimSpace(req.Placement.HomeNodeID)
	}
	if homeNodeID != "" && homeNodeID != s.cfgNodeID() {
		writeAPIError(w, http.StatusBadRequest, "invalid_request",
			"远程 Placement 已下线：跨机器协作请使用工作组",
			map[string]any{"home_node_id": homeNodeID})
		return
	}
	if store.NormalizeAgentOrigin(req.Origin) == store.AgentOriginRemote {
		writeAPIError(w, http.StatusBadRequest, "invalid_request",
			"远程 Placement 已下线：跨机器协作请使用工作组",
			map[string]any{"origin": store.AgentOriginRemote})
		return
	}

	tplID := strings.TrimSpace(req.TemplateID)
	var tpl *agenttemplate.Template
	// 无完整 defaults 时才需要加载模板做种子；有 defaults 时 template_id 仅溯源，不强制模板存在。
	if tplID != "" && req.Defaults == nil {
		loaded, err := s.templateLoader().Get(tplID)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "template_not_found", err.Error(), nil)
			return
		}
		tpl = &loaded
	} else if tplID != "" {
		if loaded, err := s.templateLoader().Get(tplID); err == nil {
			tpl = &loaded
		}
	}

	// 完整设置：前端展开模板后提交 defaults；无 defaults 时用模板种子或内置默认。
	var baseDefaults map[string]any
	fullSettings := req.Defaults != nil
	if fullSettings {
		baseDefaults = agentruntime.MergeDefaults(nil, req.Defaults)
	} else if tpl != nil {
		baseDefaults = agentruntime.MergeDefaults(tpl.Defaults, nil)
	} else {
		baseDefaults = defaultAgentCreationDefaults()
	}

	// soul/custom 正文：优先请求 defaults，其次模板预设；写入侧车表，不进 snapshot。
	soulMD, customMD := agenttemplate.PromptBodiesFromDefaults(baseDefaults)
	if soulMD == "" && customMD == "" && tpl != nil {
		soulMD, customMD = agenttemplate.PromptBodiesFromDefaults(tpl.Defaults)
	}
	agenttemplate.StripPromptBodiesFromDefaults(baseDefaults)

	name := strings.TrimSpace(req.DisplayName)
	if name == "" && tpl != nil {
		name = strings.TrimSpace(tpl.DisplayName)
		if name == "" {
			name = tpl.ID
		}
	}
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "display_name is required", nil)
		return
	}

	agentID, err := generateAgentInstanceID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_id_failed", err.Error(), nil)
		return
	}

	snapRaw, err := marshalAgentSnapshot(tplID, baseDefaults)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_encode_failed", err.Error(), nil)
		return
	}
	now := time.Now().UTC()
	rec := store.AgentRecord{
		AgentID:        agentID,
		DisplayName:    name,
		TemplateID:     tplID,
		Origin:         store.NormalizeAgentOrigin(req.Origin),
		SandboxEnabled: false,
		SandboxBackend: "process",
		ConfigSnapshot: snapRaw,
		HostJSON:       encodeJSONRaw(localHostPayload()),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.agents.Save(r.Context(), rec); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_save_failed", err.Error(), nil)
		return
	}
	if err := s.ensureAgentWorkspace(agentID); err != nil {
		s.logger.Warn("agent workspace create failed", "agent_id", agentID, "error", err)
	}
	if _, err := s.agents.EnsureAgentPolicy(r.Context(), agentID, s.runtimeDir()); err != nil {
		s.logger.Warn("agent policy seed failed", "agent_id", agentID, "error", err)
	}
	if _, err := s.agents.EnsureAgentPromptContext(r.Context(), agentID, s.runtimeDir()); err != nil {
		s.logger.Warn("agent prompt context seed failed", "agent_id", agentID, "error", err)
	} else if soulMD != "" || customMD != "" {
		if err := s.applyPromptContextBodies(r.Context(), agentID, soulMD, customMD); err != nil {
			s.logger.Warn("agent prompt context preset apply failed", "agent_id", agentID, "error", err)
		}
	}
	if s.sessions != nil {
		if err := s.reloadAgentRuntime(r.Context(), rec); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_runtime_failed", err.Error(), nil)
			return
		}
	}
	if err := s.syncBrowserCompanion(r.Context(), rec); err != nil {
		s.logger.Warn("browser companion sync failed", "agent_id", agentID, "error", err)
	}
	// 重新读取以带上 companion meta。
	if updated, err := s.agents.Get(r.Context(), agentID); err == nil && updated != nil {
		rec = *updated
	}
	writeJSON(w, http.StatusOK, agentViewFromRecord(rec))
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	list, err := s.agents.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_list_failed", err.Error(), nil)
		return
	}
	views := make([]agentView, 0, len(list))
	lastActiveAt := s.loadAgentLastActiveAt(r.Context())
	for _, rec := range list {
		if store.NormalizeAgentOrigin(rec.Origin) == store.AgentOriginRemote {
			// D5 Cut6：列表时归档遗留 remote stub（不依赖 Manage Control DELETE）。
			if err := s.agents.SoftDelete(r.Context(), rec.AgentID); err != nil {
				if s.logger != nil {
					s.logger.Warn("remote stub soft-delete failed", "agent_id", rec.AgentID, "error", err)
				}
			} else if s.sessions != nil {
				_, _ = s.sessions.Delete(rec.AgentID)
			}
			continue
		}
		if isHiddenCompanionAgent(rec) {
			continue
		}
		// 存量：已启用 browser 组但尚未创建伴生时，列表时尽力补齐。
		if err := s.syncBrowserCompanion(r.Context(), rec); err != nil {
			if s.logger != nil {
				s.logger.Warn("browser companion sync on list failed", "agent_id", rec.AgentID, "error", err)
			}
		} else if updated, err := s.agents.Get(r.Context(), rec.AgentID); err == nil && updated != nil {
			rec = *updated
		}
		view := s.enrichAgentNotify(agentViewFromRecord(rec))
		if activeAt, ok := lastActiveAt[view.AgentID]; ok && !activeAt.IsZero() {
			view.LastActiveAt = activeAt.UTC().Format(time.RFC3339Nano)
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": views})
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("agent_id"))
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
		return
	}
	if rec == nil || rec.Archived {
		s.writeAgentNotFound(w, id)
		return
	}
	if s.retireRemoteStubIfNeeded(r.Context(), w, rec) {
		return
	}
	view := s.enrichAgentNotify(agentViewFromRecord(*rec))
	if activeAt, ok := s.loadAgentLastActiveAt(r.Context())[view.AgentID]; ok && !activeAt.IsZero() {
		view.LastActiveAt = activeAt.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, view)
}

// loadAgentLastActiveAt reads the runtime snapshot timestamps separately from
// the agent configuration timestamps. The latter changes when settings are
// edited, while the former advances when the conversation is persisted.
func (s *Server) loadAgentLastActiveAt(ctx context.Context) map[string]time.Time {
	result := make(map[string]time.Time)
	if s == nil || s.sessions == nil {
		return result
	}
	summaries, err := s.sessions.ListPersisted(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("list persisted sessions for agent activity failed", "error", err)
		}
		return result
	}
	for _, summary := range summaries {
		if strings.TrimSpace(summary.AgentID) == "" || summary.UpdatedAt.IsZero() {
			continue
		}
		result[summary.AgentID] = summary.UpdatedAt
	}
	return result
}

// enrichAgentNotify 附加未读 / HITL 等通知字段（托盘待办同步用）。
func (s *Server) enrichAgentNotify(v agentView) agentView {
	if s.sessions == nil || strings.TrimSpace(v.AgentID) == "" {
		return v
	}
	notify := s.sessions.NotificationState(v.AgentID)
	v.NotifySeq = notify.NotifySeq
	v.AckSeq = notify.AckSeq
	v.HasUnread = notify.HasUnread
	v.HasPendingHITL = notify.HasPendingHITL
	v.PendingHITLItems = notify.PendingHITLItems
	if s.sessions.Get(v.AgentID) != nil {
		v.Active = true
		_, hasActiveTurn, turnState, _ := s.sessions.RuntimeInfo(v.AgentID)
		v.HasActiveTurn = hasActiveTurn
		v.RunTurnPhase = turn.RunTurnPhase(turnState)
	} else if notify.HasPendingHITL {
		v.RunTurnPhase = "waiting_hitl"
	}
	return v
}

type patchAgentRequest struct {
	DisplayName *string        `json:"display_name"`
	LLMActive   *string        `json:"llm_active"` // 兼容快捷字段；等价于 defaults.llm.active
	Defaults    map[string]any `json:"defaults"`   // 深合并进快照
}

func (s *Server) handlePatchAgent(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("agent_id"))
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
		return
	}
	if rec == nil || rec.Archived {
		s.writeAgentNotFound(w, id)
		return
	}
	if s.retireRemoteStubIfNeeded(r.Context(), w, rec) {
		return
	}
	var req patchAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if req.DisplayName == nil && req.LLMActive == nil && req.Defaults == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_patch", "no patch fields", nil)
		return
	}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if name == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_patch", "display_name cannot be empty", nil)
			return
		}
		rec.DisplayName = name
	}

	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_invalid", err.Error(), nil)
		return
	}
	if snap.Defaults == nil {
		snap.Defaults = map[string]any{}
	}
	oldToolGroups := agentruntime.EnabledToolGroups(snap)
	runtimeDirty := false

	if req.Defaults != nil {
		snap.Defaults = agentruntime.MergeDefaults(snap.Defaults, req.Defaults)
		runtimeDirty = true
	}
	if req.LLMActive != nil {
		active := strings.TrimSpace(*req.LLMActive)
		if active == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_patch", "llm_active cannot be empty", nil)
			return
		}
		if s.cfg != nil {
			if _, ok := s.cfg.LLM.GetProfile(active); !ok {
				writeAPIError(w, http.StatusBadRequest, "invalid_patch", fmt.Sprintf("llm profile %q not found", active), nil)
				return
			}
		}
		llmMap, _ := snap.Defaults["llm"].(map[string]any)
		if llmMap == nil {
			llmMap = map[string]any{}
		}
		llmMap["active"] = active
		snap.Defaults["llm"] = llmMap
		runtimeDirty = true
	}

	if runtimeDirty {
		raw, err := marshalAgentSnapshot(snap.TemplateID, snap.Defaults)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_encode_failed", err.Error(), nil)
			return
		}
		rec.ConfigSnapshot = raw
		rec.SandboxEnabled = false
		rec.SandboxBackend = "process"
	}
	rec.UpdatedAt = time.Now().UTC()
	if err := s.agents.Save(r.Context(), *rec); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_save_failed", err.Error(), nil)
		return
	}
	// Save assigns the next independent runtime_revision. Reload and response
	// handling must use the persisted value rather than the pre-save record.
	if updated, err := s.agents.Get(r.Context(), id); err == nil && updated != nil {
		rec = updated
	}
	if runtimeDirty && s.sessions != nil {
		runtimeApplied := true
		newToolGroups := agentruntime.EnabledToolGroups(snap)
		if agentruntime.ToolsetShrinks(oldToolGroups, newToolGroups) {
			s.sessions.NotifyToolsetChanged(id)
		}
		_, active, state, _ := s.sessions.RuntimeInfo(id)
		if active {
			runtimeApplied = false
			s.logger.Info("agent runtime reload deferred after patch",
				"agent_id", id,
				"runtime_revision", rec.RuntimeRevision,
				"turn_state", state,
			)
		} else if err := s.reloadAgentRuntime(r.Context(), *rec); err != nil {
			runtimeApplied = false
			s.logger.Warn("agent runtime reload after patch failed", "agent_id", id, "error", err)
		}
		s.publishRuntimeConfigChanged(id, "agent_snapshot", runtimeApplied)
	}
	if runtimeDirty {
		if err := s.syncBrowserCompanion(r.Context(), *rec); err != nil {
			s.logger.Warn("browser companion sync after patch failed", "agent_id", id, "error", err)
		}
		if updated, err := s.agents.Get(r.Context(), id); err == nil && updated != nil {
			rec = updated
		}
	}
	writeJSON(w, http.StatusOK, agentViewFromRecord(*rec))
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("agent_id"))
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
		return
	}
	if rec == nil || rec.Archived {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
		return
	}
	if err := s.softDeleteAgentCascade(r.Context(), id); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "cannot be deleted directly") {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", msg, map[string]any{"agent_id": id})
			return
		}
		writeAPIError(w, http.StatusNotFound, "agent_not_found", msg, map[string]any{"agent_id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id})
}

func (s *Server) ensureAgentWorkspace(agentID string) error {
	if s.cfg == nil {
		return nil
	}
	root := filepath.Join(s.cfg.AgentsDir(), agentID)
	for _, sub := range []string{"data", "history", "memory"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func generateAgentInstanceID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("agt-%x", b), nil
}

// ensureAgentRuntime 按 agents.db 快照把 Agent 装入内存（CreateWithOptions）。
// 若内存已有但 runtime_revision 落后，会在新 runtime 构建成功后交换。
func (s *Server) ensureAgentRuntime(ctx context.Context, agentID string) error {
	return s.ensureAgentRuntimeOpts(ctx, agentID, false)
}

func (s *Server) ensureAgentRuntimeOpts(ctx context.Context, agentID string, forceReload bool) error {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	if s.agents == nil {
		return fmt.Errorf("agents store not configured")
	}
	if s.sessions == nil {
		return fmt.Errorf("sessions manager not configured")
	}
	rec, err := s.agents.Get(ctx, id)
	if err != nil {
		return err
	}
	if rec == nil || rec.Archived {
		return fmt.Errorf("agent_not_found")
	}
	if store.NormalizeAgentOrigin(rec.Origin) == store.AgentOriginRemote {
		s.archiveRetiredRemoteStub(ctx, id)
		return fmt.Errorf("agent_not_found")
	}
	rev := rec.RuntimeRevision
	if rev <= 0 {
		// Old databases are migrated with a default revision. Keep this fallback
		// for stores created by older test fixtures that do not expose it yet.
		rev = 1
	}
	if !forceReload && s.hasRuntimeReloadPending(id) {
		if s.sessions.Get(id) != nil {
			if _, active, state, _ := s.sessions.RuntimeInfo(id); active {
				s.logger.Info("pending runtime reload remains deferred", "agent_id", id, "turn_state", state)
				return nil
			}
		}
		if err := s.reloadAgentRuntime(ctx, *rec); err != nil {
			return err
		}
		s.clearRuntimeReloadPending(id)
		return nil
	}
	if !forceReload && s.sessions.Get(id) != nil {
		if s.sessions.RuntimeRevision(id) == rev {
			// Runtime 已经加载并不代表进程级 LLM 仍然属于当前 Agent：
			// 切换 Agent 后，另一个 Agent 的配置可能已经切换了全局 active
			// profile。即使 runtime revision 没变，也必须重新应用当前绑定。
			if err := s.applyAgentLLMProfile(*rec); err != nil {
				return err
			}
			return nil
		}
		if _, active, state, _ := s.sessions.RuntimeInfo(id); active {
			s.logger.Info("agent runtime reload deferred until turn idle",
				"agent_id", id,
				"runtime_revision", rev,
				"turn_state", state,
			)
			return nil
		}
	}
	return s.reloadAgentRuntime(ctx, *rec)
}

func (s *Server) reloadAgentRuntime(ctx context.Context, rec store.AgentRecord) error {
	id := strings.TrimSpace(rec.AgentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	if s.sessions == nil {
		return fmt.Errorf("sessions manager not configured")
	}
	snapParsed, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return fmt.Errorf("parse agent snapshot: %w", err)
	}
	// 装入/聚焦 Agent 时应用其绑定的 LLM 档案（进程级共享 LLM 的过渡方案）。
	if err := s.applyAgentLLMProfile(rec); err != nil {
		s.logger.Warn("apply agent-bound llm failed", "agent_id", id, "error", err)
	}
	if err := s.ensureAgentWorkspace(id); err != nil {
		s.logger.Warn("agent workspace ensure failed", "agent_id", id, "error", err)
	}
	var policyEngine *policy.Engine
	if s.agents != nil {
		engine, err := s.agents.LoadAgentPolicyEngine(ctx, id, s.runtimeDir())
		if err != nil {
			s.logger.Warn("agent policy load failed", "agent_id", id, "error", err)
		} else {
			policyEngine = engine
		}
	}
	built, err := agentruntime.Build(agentruntime.BuildParams{
		NodeCFG:  s.cfg,
		BaseTurn: s.sessions.DefaultTurnOptions(),
		AgentID:  id,
		Snapshot: snapParsed,
		MCP:      s.mcpManager,
	})
	if err != nil {
		return fmt.Errorf("build agent runtime: %w", err)
	}
	s.attachNodeRuntimeDeps(built.Registry, id)
	rev := rec.RuntimeRevision
	if rev <= 0 {
		rev = 1
	}
	built.TurnOptions.ConfigRevision = rev // compatibility for older observers
	built.TurnOptions.RuntimeRevision = rev
	if s.cfg != nil {
		built.TurnOptions.PreferredName = s.cfg.PreferredName()
	}
	if s.agents != nil {
		if pc, err := s.agents.EnsureAgentPromptContext(ctx, id, s.runtimeDir()); err != nil {
			s.logger.Warn("agent prompt context load failed", "agent_id", id, "error", err)
		} else {
			built.TurnOptions.PromptContent = promptContentFromRecord(pc)
			scope := agentruntime.LongTermScopeFromDefaults(snapParsed)
			built.TurnOptions.LongTermStore = &agentLongTermStore{
				agents:     s.agents,
				agentID:    id,
				runtimeDir: s.runtimeDir(),
				scope:      scope,
			}
		}
	}
	// Runtime digest is an identity of the built model-visible inputs. The
	// per-Turn snapshot also records the exact prompt/tool digests, so a live
	// memory refresh can be distinguished from a runtime rebuild.
	var promptSeed string
	if built.TurnOptions.PromptContent != nil {
		if raw, err := json.Marshal(built.TurnOptions.PromptContent); err == nil {
			promptSeed = string(raw)
		}
	}
	built.TurnOptions.RuntimeDigest = turn.RuntimeDigestFromInputs(
		snapParsed,
		promptSeed,
		built.Registry.Definitions(),
	)
	// Build and hydrate the replacement before swapping the manager entry. A
	// failed load/start therefore leaves the previous good runtime serving
	// requests instead of creating a release-then-create gap.
	if _, _, err := s.sessions.ReplaceWithOptions(id, built.TurnOptions, built.Registry, policyEngine); err != nil {
		return err
	}
	s.clearRuntimeReloadPending(id)
	s.logger.Info("agent runtime ready", "agent_id", id, "fs_root", built.FSRoot, "tool_groups", built.ToolGroups)
	if !agentruntime.IsBrowserCompanionRecord(rec.ConfigSnapshot) && !agentruntime.IsCompanionBrowserAgentID(id) {
		if err := s.syncBrowserCompanion(ctx, rec); err != nil {
			s.logger.Warn("browser companion sync on reload failed", "agent_id", id, "error", err)
		}
	}
	return nil
}

// applyAgentLLMProfile keeps the process-level LLM selection aligned with the
// Agent being focused. Runtime loading can be skipped when the Agent revision
// is unchanged, but the active profile is still shared by the process and may
// have been changed while another Agent was selected.
func (s *Server) applyAgentLLMProfile(rec store.AgentRecord) error {
	if s == nil || s.cfg == nil {
		return nil
	}
	snapParsed, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return fmt.Errorf("parse agent snapshot: %w", err)
	}
	active := agentruntime.LLMActiveFromDefaults(snapParsed)
	if active == "" || active == s.cfg.LLM.ActiveProfileID() {
		return nil
	}
	return s.switchActiveLLMProfile(active)
}

func (s *Server) handleAgentEnsure(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if err := s.ensureAgentRuntime(r.Context(), id); err != nil {
		if err.Error() == "agent_not_found" {
			s.writeAgentNotFound(w, id)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", err.Error(), map[string]any{"agent_id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id})
}

func (s *Server) handleAgentReload(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if err := s.ensureAgentRuntimeOpts(r.Context(), id, true); err != nil {
		if err.Error() == "agent_not_found" {
			s.writeAgentNotFound(w, id)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "agent_reload_failed", err.Error(), map[string]any{"agent_id": id})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id, "reloaded": true})
}

// withAgentRuntime：校验/装入 Agent runtime 后调用下游（path 使用 agent_id）。
// agents store 未配置时（单测 WithSkipStore）直接放行，由下游按 runtime/DB 处理。
func (s *Server) withAgentRuntime(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("agent_id"))
		if id == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
			return
		}
		if s.agents != nil {
			if err := s.ensureAgentRuntime(r.Context(), id); err != nil {
				if err.Error() == "agent_not_found" {
					s.writeAgentNotFound(w, id)
					return
				}
				writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", err.Error(), map[string]any{"agent_id": id})
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) handleAgentHydrate(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentHydrateImpl)(w, r)
}

func (s *Server) handleAgentExecutionEvents(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentExecutionEventsImpl)(w, r)
}

type executionEventResponse struct {
	ID             int64  `json:"id"`
	AgentID        string `json:"agent_id"`
	SessionID      string `json:"session_id"`
	ProcessID      string `json:"process_id"`
	ProcessSeq     uint64 `json:"process_seq"`
	EventType      string `json:"event_type"`
	Stream         string `json:"stream,omitempty"`
	TurnID         string `json:"turn_id,omitempty"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	TargetKind     string `json:"target_kind,omitempty"`
	TargetID       string `json:"target_id,omitempty"`
	PolicyDecision string `json:"policy_decision,omitempty"`
	ApprovalID     string `json:"approval_id,omitempty"`
	RiskLevel      string `json:"risk_level,omitempty"`
	CommandDigest  string `json:"command_digest,omitempty"`
	OutputBytes    int64  `json:"output_bytes,omitempty"`
	ExitCode       *int   `json:"exit_code,omitempty"`
	ExitError      string `json:"exit_error,omitempty"`
	CreatedAt      string `json:"created_at"`
}

func (s *Server) handleAgentExecutionEventsImpl(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "store_unavailable", "execution event audit is unavailable", nil)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer", nil)
			return
		}
		limit = parsed
	}
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	events, err := s.store.ListExecutionEvents(r.Context(), agentID, limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "execution_event_list_failed", err.Error(), nil)
		return
	}
	items := make([]executionEventResponse, 0, len(events))
	for _, event := range events {
		items = append(items, executionEventResponse{
			ID:             event.ID,
			AgentID:        event.AgentID,
			SessionID:      event.SessionID,
			ProcessID:      event.ProcessID,
			ProcessSeq:     event.ProcessSeq,
			EventType:      event.EventType,
			Stream:         event.Stream,
			TurnID:         event.TurnID,
			ToolCallID:     event.ToolCallID,
			TargetKind:     event.TargetKind,
			TargetID:       event.TargetID,
			PolicyDecision: event.PolicyDecision,
			ApprovalID:     event.ApprovalID,
			RiskLevel:      event.RiskLevel,
			CommandDigest:  event.CommandDigest,
			OutputBytes:    event.OutputBytes,
			ExitCode:       event.ExitCode,
			ExitError:      event.ExitError,
			CreatedAt:      event.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": agentID,
		"events":   items,
	})
}

func (s *Server) handleAgentCancel(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentCancelImpl)(w, r)
}

func (s *Server) handleAgentContext(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentContextImpl)(w, r)
}

func (s *Server) handleAgentAck(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentAckImpl)(w, r)
}

func (s *Server) handleAgentClearContext(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentClearContextImpl)(w, r)
}

func (s *Server) handleAgentCompress(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentCompressImpl)(w, r)
}

func (s *Server) handleAgentListSkills(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentListSkillsImpl)(w, r)
}

func (s *Server) handleAgentLoadSkill(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentLoadSkillImpl)(w, r)
}

func (s *Server) handleAgentUnloadSkill(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleAgentUnloadSkillImpl)(w, r)
}

func (s *Server) handleAgentListChildAgents(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleListChildAgents)(w, r)
}

func (s *Server) handleAgentGetChildAgent(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleGetChildAgent)(w, r)
}

func (s *Server) handleAgentCancelChildAgent(w http.ResponseWriter, r *http.Request) {
	s.withAgentRuntime(s.handleCancelChildAgent)(w, r)
}
