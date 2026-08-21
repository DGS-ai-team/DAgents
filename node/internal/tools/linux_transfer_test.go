package tools

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestStrictTransferPathRequiresWorkspaceRelativePath(t *testing.T) {
	root := t.TempDir()
	if _, err := strictTransferPath(root, "../outside.txt", false); err == nil {
		t.Fatal("expected path escape to be rejected")
	}
	if _, err := strictTransferPath(root, root+"\\outside.txt", false); err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
	if got, err := strictTransferPath(root, "nested\\file.txt", false); err != nil || !strings.HasPrefix(got, root) {
		t.Fatalf("relative path = %q, err = %v", got, err)
	}
}

func TestLinuxTransferRejectsUnapprovedChannelBeforeSFTP(t *testing.T) {
	root := t.TempDir()
	localPath := "payload.txt"
	if err := os.WriteFile(root+string(os.PathSeparator)+localPath, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "prod", Host: "127.0.0.1", Port: 22, Username: "deploy",
			CredentialID: "cred", HostKeyPolicy: "pinned", HostKeyRef: "SHA256:test", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "secret", Enabled: true},
	}, func(context.Context, string) (string, error) { return "secret", nil }).WithBindingResolver(testLinuxBindingResolver{
		binding: LinuxChannelBinding{AgentID: "agent-1", ChannelID: "prod", Enabled: true, ApprovalMode: "require_approval"},
	})
	manager := NewLinuxTransferManager(provider, root, 1, nil)
	_, err := manager.Submit(context.Background(), LinuxTransferRequest{
		AgentID: "agent-1", ChannelID: "prod", Direction: "upload",
		LocalPath: localPath, RemotePath: "/tmp/payload.txt",
	})
	if err == nil || !strings.Contains(err.Error(), "requires approval") {
		t.Fatalf("expected pre-SFTP approval rejection, got %v", err)
	}
}

func TestLinuxFileTransferToolDefinitions(t *testing.T) {
	defs := linuxFileTransferToolDefs()
	if len(defs) != 2 {
		t.Fatalf("definitions = %d", len(defs))
	}
	for _, def := range defs {
		if def.Function.Name != "linux_file_upload" && def.Function.Name != "linux_file_download" {
			t.Fatalf("unexpected tool %q", def.Function.Name)
		}
		params := def.Function.Parameters
		properties, ok := params["properties"].(map[string]any)
		if !ok || properties[CallPurposeKey] == nil || properties["config_id"] == nil {
			t.Fatalf("tool %q missing common properties: %#v", def.Function.Name, params)
		}
	}
}

func TestLinuxExecToolDefinitionUsesTerminalConfigID(t *testing.T) {
	params := linuxExecToolDef()[0].Function.Parameters
	properties, ok := params["properties"].(map[string]any)
	if !ok || properties["config_id"] == nil || properties["channel_id"] != nil {
		t.Fatalf("linux_exec should expose config_id only: %#v", params)
	}
	required, ok := params["required"].([]string)
	if !ok || !containsRequiredString(required, "config_id") || !containsRequiredString(required, "command") {
		t.Fatalf("linux_exec required=%#v", params["required"])
	}
}

func containsRequiredString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestLinuxTransferRequestValidation(t *testing.T) {
	if err := validateLinuxTransferRequest(LinuxTransferRequest{AgentID: "a", ChannelID: "c", Direction: "upload", LocalPath: "x", RemotePath: "/tmp/x"}); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := validateLinuxTransferRequest(LinuxTransferRequest{AgentID: "a", ChannelID: "c", Direction: "copy", LocalPath: "x", RemotePath: "/tmp/x"}); err == nil {
		t.Fatal("expected invalid direction")
	}
	manager := NewLinuxTransferManager(&LinuxShellProvider{}, t.TempDir(), 99, nil)
	if manager.MaxConcurrent() != 8 {
		t.Fatalf("max concurrency should be capped at 8, got %d", manager.MaxConcurrent())
	}
	_, err := manager.Submit(context.Background(), LinuxTransferRequest{
		AgentID: "a", ChannelID: "c", Direction: "upload", LocalPath: "x", RemotePath: "/tmp/x",
	})
	if err == nil || !strings.Contains(err.Error(), "resolver") {
		t.Fatalf("expected unavailable resolver error, got %v", err)
	}
}
