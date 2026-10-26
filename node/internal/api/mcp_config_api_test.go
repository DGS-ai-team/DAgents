package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestMCPConfigHTTPRedactsAndRoundTripsSecretsAndDisabledState(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	secret := "synthetic-mcp-secret-never-use"
	envSecret := "synthetic-mcp-env-secret-never-use"
	mcpStore, err := store.OpenMCPServers(filepath.Join(srv.cfg.RuntimeRoot, "mcp-servers.db"), srv.cfg.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mcpStore.Close() })
	srv.mcpServers = mcpStore
	cfg := mcp.ServerConfig{
		ID: "fixture", DisplayName: "Fixture", Transport: mcp.TransportStreamableHTTP,
		URL: "http://127.0.0.1:1/mcp", HeaderValues: map[string]string{"Authorization": secret},
		EnabledTools: []string{"echo"}, Enabled: false,
	}
	if err := mcpStore.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	stdioCfg := mcp.ServerConfig{
		ID: "fixture-stdio", DisplayName: "Fixture stdio", Transport: mcp.TransportStdio,
		Command: "synthetic-mcp-must-not-launch", EnvValues: map[string]string{"FIXTURE_TOKEN": envSecret}, Enabled: false,
	}
	if err := mcpStore.Save(context.Background(), stdioCfg); err != nil {
		t.Fatal(err)
	}
	if err := srv.mcpManager.Configure([]mcp.ServerConfig{cfg, stdioCfg}); err != nil {
		t.Fatal(err)
	}

	request := func(method, path string, body any) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			encoded, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			reader = bytes.NewReader(encoded)
		}
		req, reqErr := http.NewRequest(method, ts.URL+path, reader)
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, doErr := http.DefaultClient.Do(req)
		if doErr != nil {
			t.Fatal(doErr)
		}
		defer resp.Body.Close()
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return resp.StatusCode, data
	}
	getConfig := func() (string, []byte) {
		t.Helper()
		status, body := request(http.MethodGet, "/v1/mcp/config", nil)
		if status != http.StatusOK {
			t.Fatalf("GET config status=%d body=%s", status, body)
		}
		var payload struct {
			ConfigText string `json:"config_text"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		return payload.ConfigText, body
	}
	putConfig := func(configText string) (int, []byte) {
		t.Helper()
		return request(http.MethodPut, "/v1/mcp/config", map[string]string{"config_text": configText})
	}

	configText, response := getConfig()
	if bytes.Contains(response, []byte(secret)) || bytes.Contains(response, []byte(envSecret)) || strings.Count(configText, mcp.RedactedSecretPlaceholder) != 2 {
		t.Fatalf("MCP HTTP projection leaked literal secret or missed placeholder: %s", response)
	}
	if !strings.Contains(configText, `"enabled": false`) || !strings.Contains(configText, `"enabled_tools": [`) || !strings.Contains(configText, `"echo"`) {
		t.Fatalf("MCP editor projection omitted service/tool enable state: %s", configText)
	}
	if status, body := putConfig(configText); status != http.StatusOK {
		t.Fatalf("round-trip PUT status=%d body=%s", status, body)
	}
	stored, err := mcpStore.Get(context.Background(), cfg.ID)
	if err != nil || stored == nil || stored.HeaderValues["Authorization"] != secret || stored.Enabled || len(stored.EnabledTools) != 1 || stored.EnabledTools[0] != "echo" {
		t.Fatalf("round-trip config=%+v err=%v", stored, err)
	}
	storedStdio, err := mcpStore.Get(context.Background(), stdioCfg.ID)
	if err != nil || storedStdio == nil || storedStdio.EnvValues["FIXTURE_TOKEN"] != envSecret || storedStdio.Enabled {
		t.Fatalf("round-trip stdio config=%+v err=%v", storedStdio, err)
	}
	view, ok := srv.mcpManager.Get(cfg.ID)
	if !ok || view.Status != mcp.StatusDisabled {
		t.Fatalf("disabled server was started during config round-trip: view=%+v ok=%v", view, ok)
	}
	stdioView, ok := srv.mcpManager.Get(stdioCfg.ID)
	if !ok || stdioView.Status != mcp.StatusDisabled {
		t.Fatalf("disabled stdio server was started during config round-trip: view=%+v ok=%v", stdioView, ok)
	}

	rotatedSecret := "synthetic-mcp-secret-rotated"
	var editable map[string]any
	if err := json.Unmarshal([]byte(configText), &editable); err != nil {
		t.Fatal(err)
	}
	servers := editable["mcpServers"].(map[string]any)
	server := servers[cfg.ID].(map[string]any)
	server["headers"].(map[string]any)["Authorization"] = rotatedSecret
	rotatedBytes, err := json.Marshal(editable)
	if err != nil {
		t.Fatal(err)
	}
	rotatedConfig := string(rotatedBytes)
	if status, body := putConfig(rotatedConfig); status != http.StatusOK {
		t.Fatalf("secret rotation PUT status=%d body=%s", status, body)
	}
	stored, err = mcpStore.Get(context.Background(), cfg.ID)
	if err != nil || stored == nil || stored.HeaderValues["Authorization"] != rotatedSecret {
		t.Fatalf("rotated config=%+v err=%v", stored, err)
	}
	configText, response = getConfig()
	if bytes.Contains(response, []byte(rotatedSecret)) {
		t.Fatalf("rotated secret leaked in GET: %s", response)
	}

	editable = nil
	if err := json.Unmarshal([]byte(configText), &editable); err != nil {
		t.Fatal(err)
	}
	servers = editable["mcpServers"].(map[string]any)
	server = servers[cfg.ID].(map[string]any)
	delete(server, "headers")
	deletedConfig, err := json.Marshal(editable)
	if err != nil {
		t.Fatal(err)
	}
	if status, body := putConfig(string(deletedConfig)); status != http.StatusOK {
		t.Fatalf("secret deletion PUT status=%d body=%s", status, body)
	}
	stored, err = mcpStore.Get(context.Background(), cfg.ID)
	if err != nil || stored == nil || len(stored.HeaderValues) != 0 {
		t.Fatalf("deleted secret was restored: config=%+v err=%v", stored, err)
	}

	unknownPlaceholder := `{"mcpServers":{"new":{"type":"streamable-http","url":"http://127.0.0.1:1/mcp","headers":{"Authorization":"` + mcp.RedactedSecretPlaceholder + `"}}}`
	if status, body := putConfig(unknownPlaceholder); status != http.StatusBadRequest {
		t.Fatalf("new server accepted redacted placeholder status=%d body=%s", status, body)
	}
	if created, err := mcpStore.Get(context.Background(), "new"); err != nil || created != nil {
		t.Fatalf("unknown placeholder created server: config=%+v err=%v", created, err)
	}
	if status, body := request(http.MethodDelete, "/v1/mcp/servers/"+cfg.ID, nil); status != http.StatusOK {
		t.Fatalf("server delete status=%d body=%s", status, body)
	}
	if deleted, err := mcpStore.Get(context.Background(), cfg.ID); err != nil || deleted != nil {
		t.Fatalf("deleted MCP server remains: config=%+v err=%v", deleted, err)
	}
	if status, body := request(http.MethodDelete, "/v1/mcp/servers/"+stdioCfg.ID, nil); status != http.StatusOK {
		t.Fatalf("stdio server delete status=%d body=%s", status, body)
	}
}

func TestMCPServerHTTPEnableDisableAndToolAllowlist(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	mcpStore, err := store.OpenMCPServers(filepath.Join(srv.cfg.RuntimeRoot, "mcp-servers.db"), srv.cfg.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mcpStore.Close() })
	srv.mcpServers = mcpStore
	var listCalls atomic.Int32
	mcpFixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode synthetic MCP request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": mcp.ProtocolVersion, "capabilities": map[string]any{}}
		case "tools/list":
			listCalls.Add(1)
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "inputSchema": map[string]any{"type": "object"}}}}
		default:
			t.Errorf("unexpected synthetic MCP method %q", req.Method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer mcpFixture.Close()

	request := func(method, path string, body any) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			encoded, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			reader = bytes.NewReader(encoded)
		}
		req, reqErr := http.NewRequest(method, ts.URL+path, reader)
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, doErr := http.DefaultClient.Do(req)
		if doErr != nil {
			t.Fatal(doErr)
		}
		defer resp.Body.Close()
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return resp.StatusCode, data
	}
	status, body := request(http.MethodPost, "/v1/mcp/servers", map[string]any{
		"id": "fixture", "display_name": "Fixture", "transport": "streamable-http",
		"url": mcpFixture.URL, "enabled": true, "enabled_tools": []string{"echo"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create MCP server status=%d body=%s", status, body)
	}
	var view map[string]any
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	if view["status"] != mcp.StatusReady || view["enabled_tool_count"] != float64(1) || listCalls.Load() < 1 {
		t.Fatalf("created MCP view=%s list_calls=%d", body, listCalls.Load())
	}

	status, body = request(http.MethodPatch, "/v1/mcp/servers/fixture", map[string]any{
		"enabled_tools": []string{},
	})
	if status != http.StatusOK {
		t.Fatalf("disable tools status=%d body=%s", status, body)
	}
	_ = json.Unmarshal(body, &view)
	if view["status"] != mcp.StatusReady || view["enabled_tool_count"] != float64(0) {
		t.Fatalf("tool allowlist was not applied: %s", body)
	}

	status, body = request(http.MethodPatch, "/v1/mcp/servers/fixture", map[string]any{"enabled": false})
	if status != http.StatusOK {
		t.Fatalf("disable service status=%d body=%s", status, body)
	}
	_ = json.Unmarshal(body, &view)
	if view["status"] != mcp.StatusDisabled {
		t.Fatalf("service did not disable: %s", body)
	}
	listCallsBeforeReenable := listCalls.Load()
	status, body = request(http.MethodPatch, "/v1/mcp/servers/fixture", map[string]any{"enabled": true})
	if status != http.StatusOK {
		t.Fatalf("re-enable service status=%d body=%s", status, body)
	}
	_ = json.Unmarshal(body, &view)
	if view["status"] != mcp.StatusReady || listCalls.Load() <= listCallsBeforeReenable {
		t.Fatalf("service did not reconnect after enable: %s list_calls=%d", body, listCalls.Load())
	}
	status, body = request(http.MethodPatch, "/v1/mcp/servers/fixture", map[string]any{"enabled_tools": []string{"echo"}})
	if status != http.StatusOK {
		t.Fatalf("re-enable tool allowlist status=%d body=%s", status, body)
	}
	_ = json.Unmarshal(body, &view)
	if view["status"] != mcp.StatusReady || view["enabled_tool_count"] != float64(1) {
		t.Fatalf("tool did not re-enable: %s", body)
	}

	status, body = request(http.MethodDelete, "/v1/mcp/servers/fixture", nil)
	if status != http.StatusOK {
		t.Fatalf("delete MCP server status=%d body=%s", status, body)
	}
	if _, ok := srv.mcpManager.Get("fixture"); ok {
		t.Fatalf("deleted MCP server remains in manager: %+v", view)
	}
}
