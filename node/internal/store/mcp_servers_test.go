package store

import (
	"context"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
)

func TestMCPServerStoreRoundTrip(t *testing.T) {
	st, err := OpenMCPServers(t.TempDir() + "/mcp.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := mcp.ServerConfig{
		ID: "demo", Command: "demo.exe", EnabledTools: []string{"echo"}, Enabled: true,
		EnvValues:    map[string]string{"DEMO_TOKEN": "plain-token"},
		HeaderValues: map[string]string{"Authorization": "Bearer plain-token"},
	}
	if err := st.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := st.db.QueryRow(`SELECT config_json FROM mcp_servers WHERE server_id = ?`, "demo").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "plain-token") {
		t.Fatalf("MCP literal credential was persisted in plaintext: %s", raw)
	}
	got, err := st.Get(context.Background(), "demo")
	if err != nil || got == nil || got.ID != cfg.ID {
		t.Fatalf("get failed: %#v %v", got, err)
	}
	if len(got.EnabledTools) != 1 || got.EnabledTools[0] != "echo" {
		t.Fatalf("enabled tools were not persisted: %#v", got.EnabledTools)
	}
	if got.EnvValues["DEMO_TOKEN"] != "plain-token" || got.HeaderValues["Authorization"] != "Bearer plain-token" {
		t.Fatalf("literal credentials were not persisted: %#v", got)
	}
	list, err := st.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list failed: %#v %v", list, err)
	}
	if err := st.Delete(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
}

func TestMCPServerStoreMigratesLegacyPlaintextValues(t *testing.T) {
	st, err := OpenMCPServers(t.TempDir() + "/mcp.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	legacy := `{"id":"legacy","command":"demo","env_values":{"TOKEN":"legacy-token"},"enabled":true}`
	if _, err := st.db.Exec(`INSERT INTO mcp_servers(server_id, config_json, created_at, updated_at) VALUES (?, ?, ?, ?)`, "legacy", legacy, "now", "now"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(context.Background(), "legacy")
	if err != nil || got == nil || got.EnvValues["TOKEN"] != "legacy-token" {
		t.Fatalf("legacy config=%#v err=%v", got, err)
	}
	var raw string
	if err := st.db.QueryRow(`SELECT config_json FROM mcp_servers WHERE server_id = ?`, "legacy").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "legacy-token") || !strings.Contains(raw, "env_value_ciphertexts") {
		t.Fatalf("legacy MCP config was not migrated: %s", raw)
	}
}
