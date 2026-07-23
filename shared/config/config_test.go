package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfigPath(t *testing.T, content string) (configPath, runtimeDir string) {
	t.Helper()
	dir := t.TempDir()
	runtimeDir = dir
	path := filepath.Join(dir, "config.yaml")
	if !strings.Contains(content, "fs_root:") {
		content = fmt.Sprintf("fs_root: %q\n", runtimeDir) + content
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, runtimeDir
}

func TestLoadFile_appliesDefaults(t *testing.T) {
	path, runtimeDir := testConfigPath(t, "node_id: test-agent\n")

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.NodeID != "test-agent" {
		t.Fatalf("node_id = %q, want test-agent", cfg.NodeID)
	}
	idFile := filepath.Join(runtimeDir, "node", "node_id")
	raw, err := os.ReadFile(idFile)
	if err != nil {
		t.Fatalf("read node_id file: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "test-agent" {
		t.Fatalf("node_id file = %q, want test-agent", raw)
	}
	if cfg.Listen.Host != DefaultListenHost {
		t.Fatalf("listen.host = %q, want %q", cfg.Listen.Host, DefaultListenHost)
	}
	if cfg.Listen.Port != DefaultListenPort {
		t.Fatalf("listen.port = %d, want %d", cfg.Listen.Port, DefaultListenPort)
	}
	wantEndpoint := "http://127.0.0.1:18765"
	if cfg.Local.Endpoint != wantEndpoint {
		t.Fatalf("local.endpoint = %q, want %q", cfg.Local.Endpoint, wantEndpoint)
	}
	if cfg.FSRoot != runtimeDir {
		t.Fatalf("fs_root = %q, want %q", cfg.FSRoot, runtimeDir)
	}
	wantDB := filepath.Join(runtimeDir, "memory", "sessions.db")
	if cfg.SessionDBPath() != wantDB {
		t.Fatalf("SessionDBPath = %q, want %q", cfg.SessionDBPath(), wantDB)
	}
	wantData := filepath.Join(runtimeDir, "data")
	if cfg.DataDir() != wantData {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir(), wantData)
	}
	wantSkills := filepath.Join(runtimeDir, "skills")
	if cfg.SkillsRoot() != wantSkills {
		t.Fatalf("SkillsRoot = %q, want %q", cfg.SkillsRoot(), wantSkills)
	}
}

func TestLoadFile_autoGeneratesNodeID(t *testing.T) {
	path, runtimeDir := testConfigPath(t, "listen:\n  port: 8080\n")

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if strings.TrimSpace(cfg.NodeID) == "" {
		t.Fatal("expected generated node_id")
	}
	idFile := filepath.Join(runtimeDir, "node", "node_id")
	raw, err := os.ReadFile(idFile)
	if err != nil {
		t.Fatalf("read node_id file: %v", err)
	}
	if strings.TrimSpace(string(raw)) != cfg.NodeID {
		t.Fatalf("file node_id = %q, cfg = %q", raw, cfg.NodeID)
	}
}

func TestLoadFile_readsNodeIDFromFile(t *testing.T) {
	path, runtimeDir := testConfigPath(t, "node_id: yaml-should-not-win\n")
	idFile := filepath.Join(runtimeDir, "node", "node_id")
	if err := os.MkdirAll(filepath.Dir(idFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(idFile, []byte("from-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.NodeID != "from-file" {
		t.Fatalf("node_id = %q, want from-file", cfg.NodeID)
	}
}

func TestLoadFile_expandsEnv(t *testing.T) {
	path, runtimeDir := testConfigPath(t, "node_id: ${TEST_DAGENTS_AGENT_ID}\n")
	t.Setenv("TEST_DAGENTS_AGENT_ID", "from-env")

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.NodeID != "from-env" {
		t.Fatalf("node_id = %q, want from-env", cfg.NodeID)
	}
	raw, err := os.ReadFile(filepath.Join(runtimeDir, "node", "node_id"))
	if err != nil {
		t.Fatalf("read node_id file: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "from-env" {
		t.Fatalf("node_id file = %q, want from-env", raw)
	}
}

func TestRawMessageHistoryEnabled_envOverridesYAML(t *testing.T) {
	path, _ := testConfigPath(t, "node_id: test-agent\nraw_message_history:\n  enabled: true\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	t.Setenv(EnvRawMessageHistoryEnabled, "false")
	if cfg.RawMessageHistoryEnabled() {
		t.Fatal("expected env false to disable journal")
	}
}

func TestRawMessageHistoryDir(t *testing.T) {
	path, runtimeDir := testConfigPath(t, "node_id: test-agent\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := filepath.Join(runtimeDir, "history")
	if cfg.RawMessageHistoryDir() != want {
		t.Fatalf("RawMessageHistoryDir = %q, want %q", cfg.RawMessageHistoryDir(), want)
	}
}

func TestLoadFile_envNodeIDOverridesFile(t *testing.T) {
	path, runtimeDir := testConfigPath(t, "")
	idFile := filepath.Join(runtimeDir, "node", "node_id")
	if err := os.MkdirAll(filepath.Dir(idFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(idFile, []byte("old-id"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvNodeID, "env-wins")

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.NodeID != "env-wins" {
		t.Fatalf("node_id = %q, want env-wins", cfg.NodeID)
	}
	raw, _ := os.ReadFile(idFile)
	if strings.TrimSpace(string(raw)) != "env-wins" {
		t.Fatalf("node_id file = %q, want env-wins", raw)
	}
}

func TestManageRegistryBaseURL_prefersRegistrationOverride(t *testing.T) {
	cfg := &Config{
		Local: LocalConfig{Endpoint: "http://127.0.0.1:18765"},
		Manage: ManageConfig{
			Registration: ManageRegistrationConfig{
				BaseURL: "http://192.168.1.10:18765",
			},
		},
	}
	if got := cfg.ManageRegistryBaseURL(); got != "http://192.168.1.10:18765" {
		t.Fatalf("ManageRegistryBaseURL = %q", got)
	}
	if cfg.ManageRegistryBaseURLIsLoopback() {
		t.Fatal("expected loopback false for LAN base_url")
	}
	cfg.Manage.Registration.BaseURL = ""
	if got := cfg.ManageRegistryBaseURL(); got != "http://127.0.0.1:18765" {
		t.Fatalf("fallback ManageRegistryBaseURL = %q", got)
	}
	if !cfg.ManageRegistryBaseURLIsLoopback() {
		t.Fatal("expected loopback true for local endpoint")
	}
}

func TestManageA2AConfigDefaults(t *testing.T) {
	cfg := &Config{
		Agent: AgentConfig{Role: "compliance"},
		Manage: ManageConfig{
			Enabled: true,
			Registration: ManageRegistrationConfig{
				IntervalSeconds: 40,
			},
		},
	}
	cfg.ApplyDefaults()
	if !cfg.ManageA2AEnabled() {
		t.Fatal("expected default a2a enabled for compliance role")
	}
	cfg.Agent.Role = "ops"
	if cfg.ManageA2AEnabled() {
		t.Fatal("expected default a2a disabled for ops role")
	}
	cfg.Agent.Role = "compliance"
	if cfg.ManageA2AInboxWait() != 25*time.Second {
		t.Fatalf("wait=%s", cfg.ManageA2AInboxWait())
	}
	if cfg.ManageA2AInboxPollInterval() != 40*time.Second {
		t.Fatalf("poll=%s", cfg.ManageA2AInboxPollInterval())
	}
	disabled := false
	cfg.Manage.A2A.Enabled = &disabled
	if cfg.ManageA2AEnabled() {
		t.Fatal("expected explicit disable")
	}
}

func TestMultimodalEnabled_defaultFalse(t *testing.T) {
	path, _ := testConfigPath(t, "node_id: test-agent\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.MultimodalEnabled() {
		t.Fatal("expected default multimodal disabled")
	}
}

func TestMultimodalEnabled_explicitTrue(t *testing.T) {
	path, _ := testConfigPath(t, "node_id: test-agent\nmultimodal:\n  enabled: true\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !cfg.MultimodalEnabled() {
		t.Fatal("expected multimodal enabled")
	}
	caps := cfg.Capabilities()
	found := false
	for _, c := range caps {
		if c == "multimodal" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("capabilities = %v, want multimodal", caps)
	}
}