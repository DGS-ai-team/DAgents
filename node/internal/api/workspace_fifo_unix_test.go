//go:build !windows

package api

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWorkspaceListingOmitsFIFO(t *testing.T) {
	s, root := workspaceTestServer(t)
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	w := workspaceRequest(s, "GET", "/v1/agents/agt-one/workspace", nil, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "pipe") {
		t.Fatalf("FIFO appeared in workspace listing: %d %s", w.Code, w.Body)
	}
}
