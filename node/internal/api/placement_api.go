package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
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
	// D5：GET /v1/peers/nodes 已移除；内部 create/delete 仍 410 供 Manage DELETE 探测。
	s.mux.HandleFunc("POST /v1/internal/placement/agents", s.handleInternalPlacementCreate)
	s.mux.HandleFunc("DELETE /v1/internal/placement/agents/{agent_id}", s.handleInternalPlacementDelete)
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
	writeAPIError(w, http.StatusGone, "placement_deprecated", "远程 Placement 入口已下线：跨机器协作请使用工作组", nil)
}

func (s *Server) handleInternalPlacementDelete(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, http.StatusGone, "placement_deprecated", "远程 Placement 入口已下线：跨机器协作请使用工作组", nil)
}
