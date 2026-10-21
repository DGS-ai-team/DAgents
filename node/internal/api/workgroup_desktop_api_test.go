package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func newDesktopWorkgroupTestServer(t *testing.T, manageHandler http.HandlerFunc) (*Server, *httptest.Server) {
	t.Helper()
	manageServer := httptest.NewServer(manageHandler)
	cfg := &config.Config{NodeID: "node-local", Manage: config.ManageConfig{Enabled: true, URL: manageServer.URL}}
	cfg.Manage.Workgroup.Enabled = new(bool)
	*cfg.Manage.Workgroup.Enabled = true
	s := &Server{cfg: cfg, mux: http.NewServeMux(), control: manage.NewControlClient(cfg)}
	s.registerWorkgroupRoutes()
	t.Cleanup(manageServer.Close)
	return s, manageServer
}

func TestDesktopWorkgroupListMergesSubscriptionState(t *testing.T) {
	s, _ := newDesktopWorkgroupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workgroups" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var rows []map[string]any
		if r.URL.Query().Get("subscribed_by") == "node-local" {
			rows = []map[string]any{{"workgroup_id": "wg-a", "display_name": "组 A", "status": "active"}}
		} else {
			rows = []map[string]any{{"workgroup_id": "wg-a", "display_name": "组 A", "status": "active"}, {"workgroup_id": "wg-b", "display_name": "组 B", "status": "configuring"}}
		}
		_ = json.NewEncoder(w).Encode(rows)
	})
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/workgroups/desktop/list", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Workgroups []struct {
			WorkgroupID string `json:"workgroup_id"`
			Subscribed  bool   `json:"subscribed"`
		} `json:"workgroups"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Workgroups) != 2 || payload.Workgroups[0].WorkgroupID != "wg-a" || !payload.Workgroups[0].Subscribed || payload.Workgroups[1].Subscribed {
		t.Fatalf("unexpected merged list: %+v", payload.Workgroups)
	}
}

func TestDesktopWorkgroupSnapshotReturnsUIStateBundle(t *testing.T) {
	s, _ := newDesktopWorkgroupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/workgroups/wg-a":
			_, _ = w.Write([]byte(`{"workgroup":{"workgroup_id":"wg-a","display_name":"调研组","status":"active"}}`))
		case r.URL.Path == "/v1/workgroups/wg-a/members":
			_, _ = w.Write([]byte(`[{"member_id":"member-1"}]`))
		case r.URL.Path == "/v1/workgroups/wg-a/timeline":
			_, _ = w.Write([]byte(`[{"event_id":"event-1"}]`))
		case r.URL.Path == "/v1/workgroups/wg-a/hitl" && r.URL.Query().Get("pending_only") == "true":
			_, _ = w.Write([]byte(`[{"hitl_id":"hitl-1"}]`))
		case r.URL.Path == "/v1/workgroups/wg-a/human-queue":
			_, _ = w.Write([]byte(`{"items":[{"queue_id":"queue-1"}],"busy":true}`))
		case r.URL.Path == "/v1/registry/agents":
			_, _ = w.Write([]byte(`{"agents":[{"agent_id":"agent-1"}]}`))
		default:
			t.Errorf("unexpected manage request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	})
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/workgroups/wg-a/desktop-snapshot?include_agents=true", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["node_id"] != "node-local" || payload["human_queue"] == nil || payload["members"] == nil || payload["events"] == nil || payload["hitl"] == nil || payload["agents"] == nil {
		t.Fatalf("incomplete snapshot: %s", rr.Body.String())
	}
}

func TestDesktopWorkgroupProgressCombinesTimelineAndQueue(t *testing.T) {
	s, _ := newDesktopWorkgroupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/workgroups/wg-a/timeline":
			_, _ = w.Write([]byte(`[{"event_id":"event-1"}]`))
		case "/v1/workgroups/wg-a/human-queue":
			_, _ = w.Write([]byte(`{"items":[{"queue_id":"queue-1"}],"busy":true}`))
		default:
			t.Errorf("unexpected manage request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	})
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/workgroups/wg-a/desktop-progress", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["events"] == nil || payload["human_queue"] == nil {
		t.Fatalf("incomplete progress snapshot: %s", rr.Body.String())
	}
}
