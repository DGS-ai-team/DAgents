package tools

import (
	"context"
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
		if !ok || properties[CallPurposeKey] == nil || properties["channel_id"] == nil {
			t.Fatalf("tool %q missing common properties: %#v", def.Function.Name, params)
		}
	}
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
