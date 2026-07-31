package api

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
)

// placementSpec / placementPayload 仅兼容旧请求与 agents.placement_json 反序列化；
// D5 Cut7：内部 placement HTTP 路由已移除。
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

func (s *Server) cfgNodeID() string {
	if s.cfg == nil {
		return ""
	}
	return s.cfg.NodeID
}
