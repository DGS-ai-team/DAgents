package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

const (
	placementControlHeader = "x-dagents-placement-control"
	ownerNodeHeader        = "x-dagents-owner-node-id"
	tokenHeader            = "x-dagents-a2a-token"
)

type placementSpec struct {
	HomeNodeID string `json:"home_node_id"`
}

type placementPayload struct {
	Role        string `json:"role"` // home | owner_ref
	OwnerNodeID string `json:"owner_node_id"`
	HomeNodeID  string `json:"home_node_id"`
	Status      string `json:"status,omitempty"`
}

type hostPayload struct {
	OSKind           string `json:"os_kind"`
	SysPlatform      string `json:"sys_platform"`
	Machine          string `json:"machine"`
	DisplayAvailable bool   `json:"display_available"`
	DisplayLabel     string `json:"display_label"`
	DisplayBackend   string `json:"display_backend,omitempty"`
	DisplayReason    string `json:"display_reason,omitempty"`
}

func localHostPayload() hostPayload {
	h := hostsnapshot.Get()
	osKind := strings.ToLower(strings.TrimSpace(h.OSKind))
	sys := strings.ToLower(strings.TrimSpace(h.SysPlatform))
	display := false
	backend := "none"
	reason := ""
	switch {
	case osKind == "windows" || sys == "windows":
		display = true
		backend = "stub"
	case osKind == "darwin" || sys == "darwin":
		display = true
		backend = "stub"
	default:
		display = strings.TrimSpace(os.Getenv("DISPLAY")) != "" || strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != ""
		if display {
			backend = "stub"
		} else {
			reason = "no_display"
		}
	}
	label := "Unknown"
	switch {
	case osKind == "windows" || sys == "windows":
		label = "Windows"
	case osKind == "darwin" || sys == "darwin":
		label = "macOS"
	case len(osKind) > 0:
		label = strings.ToUpper(osKind[:1]) + osKind[1:]
	case len(sys) > 0:
		label = strings.ToUpper(sys[:1]) + sys[1:]
	}
	return hostPayload{
		OSKind:           firstNonEmpty(osKind, sys, "unknown"),
		SysPlatform:      firstNonEmpty(sys, osKind),
		Machine:          h.Machine,
		DisplayAvailable: display,
		DisplayLabel:     label,
		DisplayBackend:   backend,
		DisplayReason:    reason,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func encodeJSONRaw(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func decodePlacement(raw json.RawMessage) placementPayload {
	var p placementPayload
	if len(raw) == 0 {
		return p
	}
	_ = json.Unmarshal(raw, &p)
	return p
}

func (s *Server) authorizePlacementControl(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get(placementControlHeader)) != "1" {
		return false
	}
	want := ""
	if s.cfg != nil {
		want = strings.TrimSpace(s.cfg.Manage.NodeToken)
	}
	got := strings.TrimSpace(r.Header.Get(tokenHeader))
	if want == "" {
		return true
	}
	return got != "" && got == want
}

func (s *Server) registerPlacementRoutes() {
	s.mux.HandleFunc("GET /v1/peers/nodes", s.handleListPeerNodes)
	s.mux.HandleFunc("POST /v1/internal/placement/agents", s.handleInternalPlacementCreate)
	s.mux.HandleFunc("DELETE /v1/internal/placement/agents/{agent_id}", s.handleInternalPlacementDelete)
}

func (s *Server) handleListPeerNodes(w http.ResponseWriter, r *http.Request) {
	if s.control == nil || s.cfg == nil || !s.cfg.Manage.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}, "self_node_id": s.cfgNodeID()})
		return
	}
	nodes, err := s.control.ListPeers(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "peers_unavailable", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "self_node_id": s.cfgNodeID()})
}

func (s *Server) cfgNodeID() string {
	if s.cfg == nil {
		return ""
	}
	return s.cfg.NodeID
}

type internalPlacementCreateRequest struct {
	DisplayName string           `json:"display_name"`
	TemplateID  string           `json:"template_id"`
	Defaults    map[string]any   `json:"defaults"`
	Sandbox     *sandboxPatch    `json:"sandbox"`
	Placement   placementPayload `json:"placement"`
}

func (s *Server) handleInternalPlacementCreate(w http.ResponseWriter, r *http.Request) {
	if !s.authorizePlacementControl(r) {
		writeAPIError(w, http.StatusUnauthorized, "placement_unauthorized", "placement control auth failed", nil)
		return
	}
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	var req internalPlacementCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "display_name is required", nil)
		return
	}
	owner := strings.TrimSpace(req.Placement.OwnerNodeID)
	home := strings.TrimSpace(req.Placement.HomeNodeID)
	if home == "" {
		home = s.cfgNodeID()
	}
	if owner == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_placement", "owner_node_id is required", nil)
		return
	}
	if home != s.cfgNodeID() {
		writeAPIError(w, http.StatusBadRequest, "invalid_placement", "home_node_id must be this node", nil)
		return
	}

	baseSandbox := agentruntime.SandboxSpec{
		Backend: "process", WorkspaceSubdir: "data", AllowBash: true, AllowNetworkTools: true,
	}
	baseDefaults := defaultAgentCreationDefaults()
	if req.Defaults != nil {
		baseDefaults = agentruntime.MergeDefaults(nil, req.Defaults)
	}
	sandboxSpec, err := applySandboxPatch(baseSandbox, req.Sandbox)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_sandbox", err.Error(), nil)
		return
	}
	if err := requireDockerSandboxReady(sandboxSpec); err != nil {
		writeSandboxReadyError(w, err)
		return
	}

	agentID, err := generateAgentInstanceID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_id_failed", err.Error(), nil)
		return
	}
	tplID := strings.TrimSpace(req.TemplateID)
	snapRaw, err := marshalAgentSnapshot(tplID, baseDefaults, sandboxSpec)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_snapshot_encode_failed", err.Error(), nil)
		return
	}
	placement := placementPayload{
		Role:        "home",
		OwnerNodeID: owner,
		HomeNodeID:  home,
	}
	host := localHostPayload()
	now := time.Now().UTC()
	rec := store.AgentRecord{
		AgentID:        agentID,
		DisplayName:    name,
		TemplateID:     tplID,
		Origin:         store.AgentOriginLocal,
		SandboxEnabled: sandboxSpec.Enabled,
		SandboxBackend: sandboxSpec.Backend,
		ConfigSnapshot: snapRaw,
		PlacementJSON:  encodeJSONRaw(placement),
		HostJSON:       encodeJSONRaw(host),
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
	}
	if s.sessions != nil {
		if err := s.reloadAgentRuntime(r.Context(), rec); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "agent_runtime_failed", err.Error(), nil)
			return
		}
	}
	writeJSON(w, http.StatusOK, agentViewFromRecord(rec))
}

func (s *Server) handleInternalPlacementDelete(w http.ResponseWriter, r *http.Request) {
	if !s.authorizePlacementControl(r) {
		writeAPIError(w, http.StatusUnauthorized, "placement_unauthorized", "placement control auth failed", nil)
		return
	}
	if s.agents == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "agents_unavailable", "agents store not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("agent_id"))
	owner := strings.TrimSpace(r.Header.Get(ownerNodeHeader))
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "agent_get_failed", err.Error(), nil)
		return
	}
	if rec == nil || rec.Archived {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": id})
		return
	}
	p := decodePlacement(rec.PlacementJSON)
	if p.Role == "home" && owner != "" && p.OwnerNodeID != "" && p.OwnerNodeID != owner {
		writeAPIError(w, http.StatusForbidden, "placement_forbidden", "only owner_node may delete", nil)
		return
	}
	if err := s.agents.SoftDelete(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", err.Error(), map[string]any{"agent_id": id})
		return
	}
	if s.sessions != nil {
		_, _ = s.sessions.Delete(id)
	} else if s.sandboxPool != nil {
		s.sandboxPool.Release(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent_id": id, "home_deleted": true})
}
