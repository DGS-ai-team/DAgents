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
	manager := NewLinuxTransferManager(provider, 1, nil)
	_, err := manager.Submit(context.Background(), LinuxTransferRequest{
		AgentID: "agent-1", ChannelID: "prod", WorkspaceRoot: root, Direction: "upload",
		LocalPath: localPath, RemotePath: "/tmp/payload.txt",
	})
	if err == nil || !strings.Contains(err.Error(), "requires approval") {
		t.Fatalf("expected pre-SFTP approval rejection, got %v", err)
	}
}

func TestLinuxTransferRequestValidation(t *testing.T) {
	if err := validateLinuxTransferRequest(LinuxTransferRequest{AgentID: "a", ChannelID: "c", WorkspaceRoot: t.TempDir(), Direction: "upload", LocalPath: "x", RemotePath: "/tmp/x"}); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := validateLinuxTransferRequest(LinuxTransferRequest{AgentID: "a", ChannelID: "c", Direction: "copy", LocalPath: "x", RemotePath: "/tmp/x"}); err == nil {
		t.Fatal("expected invalid direction")
	}
	manager := NewLinuxTransferManager(&LinuxShellProvider{}, 99, nil)
	if manager.MaxConcurrent() != 8 {
		t.Fatalf("max concurrency should be capped at 8, got %d", manager.MaxConcurrent())
	}
	_, err := manager.Submit(context.Background(), LinuxTransferRequest{
		AgentID: "a", ChannelID: "c", WorkspaceRoot: t.TempDir(), Direction: "upload", LocalPath: "x", RemotePath: "/tmp/x",
	})
	if err == nil || !strings.Contains(err.Error(), "resolver") {
		t.Fatalf("expected unavailable resolver error, got %v", err)
	}
}
