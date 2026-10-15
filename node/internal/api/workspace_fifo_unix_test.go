//go:build !windows

package api

import (
	"net/http"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWorkspaceHTTPRejectsFIFOWithoutBlocking(t *testing.T) {
	s, root := workspaceTestServer(t)
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/file?path=pipe", nil, nil)
	if w.Code < 400 {
		t.Fatalf("FIFO read status=%d", w.Code)
	}
}
