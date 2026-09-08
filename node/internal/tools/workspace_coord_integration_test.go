package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
)

func TestRegistriesShareWorkspaceWriteCoordinator(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "same.txt")
	r1, err := NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	c := workspacecoord.New()
	r1.SetWorkspaceCoordinator(c)
	r2.SetWorkspaceCoordinator(c)
	lease, err := r1.acquireWorkspaceWrite(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	body, _ := json.Marshal(writeFileArgs{Path: target, Content: "blocked"})
	out, err := r2.execWriteFile(ctx, body)
	if err == nil || !strings.Contains(err.Error(), "workspace_busy") || out != "" {
		t.Fatalf("write result=%q err=%v", out, err)
	}
	lease.Release()
	if _, err = r2.Execute(context.Background(), "write_file", string(body)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "blocked" {
		t.Fatalf("target=%q", got)
	}
	other := filepath.Join(root, "other.txt")
	otherBody, _ := json.Marshal(writeFileArgs{Path: other, Content: "independent"})
	lease, err = r1.acquireWorkspaceWrite(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if _, err = r2.Execute(context.Background(), "write_file", string(otherBody)); err != nil {
		t.Fatalf("unrelated write blocked: %v", err)
	}
}
