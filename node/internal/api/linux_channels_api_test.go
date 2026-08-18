package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestLinuxChannelRoutesPersistAndReplaceBindings(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	linuxStore, err := store.OpenLinuxChannels(t.TempDir() + "/linux.db")
	if err != nil {
		t.Fatal(err)
	}
	defer linuxStore.Close()
	srv.linuxChannels = linuxStore

	postJSON := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(data, &out)
		return resp.StatusCode, out
	}

	credentialBody := map[string]any{}
	if status, body := postJSON(http.MethodPost, "/v1/linux/credentials", map[string]any{
		"credential_id": "client-supplied-id", "auth_type": "password", "secret_ref": "env:SSH_PASSWORD",
	}); status != http.StatusCreated || body["credential_id"] == "client-supplied-id" || !strings.HasPrefix(body["credential_id"].(string), "cred_") {
		t.Fatalf("credential status=%d body=%v", status, body)
	} else {
		credentialBody = body
	}
	credentialID := credentialBody["credential_id"].(string)
	channelBody := map[string]any{}
	if status, body := postJSON(http.MethodPost, "/v1/linux/channels", map[string]any{
		"channel_id": "client-supplied-channel", "host": "127.0.0.1", "username": "root", "credential_id": credentialID,
	}); status != http.StatusCreated || body["channel_id"] == "client-supplied-channel" || !strings.HasPrefix(body["channel_id"].(string), "channel_") {
		t.Fatalf("channel status=%d body=%v", status, body)
	} else {
		channelBody = body
	}
	channelID := channelBody["channel_id"].(string)
	if status, body := postJSON(http.MethodPut, "/v1/agents/agent-a/linux-channels", map[string]any{
		"bindings": []map[string]any{{"channel_id": channelID, "enabled": true}},
	}); status != http.StatusOK || body["agent_id"] != "agent-a" {
		t.Fatalf("binding status=%d body=%v", status, body)
	}
	if status, body := postJSON(http.MethodPut, "/v1/agents/agent-a/linux-channels", map[string]any{
		"bindings": []map[string]any{{"channel_id": "missing", "enabled": true}},
	}); status != http.StatusBadRequest {
		t.Fatalf("invalid replacement status=%d body=%v", status, body)
	}
	bindings, err := linuxStore.ListBindings(t.Context(), "agent-a")
	if err != nil || len(bindings) != 1 || bindings[0].ChannelID != channelID {
		t.Fatalf("binding replacement was not atomic: %+v err=%v", bindings, err)
	}
}
