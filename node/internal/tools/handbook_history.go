package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/handbookfs"
)

// HandbookHistory exposes filesystem history to the control plane without
// making .history reachable through model-facing file tools.
func (r *Registry) HandbookHistory(ctx context.Context, raw string) ([]handbookfs.Entry, error) {
	if r == nil || r.handbookFS == nil || !isHandbookPath(strings.TrimSpace(raw)) {
		return nil, fmt.Errorf("handbook history unavailable")
	}
	path, err := r.resolvePath(raw)
	if err != nil {
		return nil, err
	}
	return r.handbookFS.History(ctx, path)
}

func (r *Registry) RestoreHandbook(ctx context.Context, raw, expectedDigest string, revision int64) (handbookfs.Entry, error) {
	if r == nil || r.handbookFS == nil || !isHandbookPath(strings.TrimSpace(raw)) {
		return handbookfs.Entry{}, fmt.Errorf("handbook history unavailable")
	}
	path, err := r.resolvePath(raw)
	if err != nil {
		return handbookfs.Entry{}, err
	}
	lease, err := r.acquireWorkspaceWrite(ctx, path)
	if err != nil {
		return handbookfs.Entry{}, fmt.Errorf("workspace_busy: %w", err)
	}
	defer lease.Release()
	return r.handbookFS.Restore(ctx, path, expectedDigest, revision)
}

func (r *Registry) handbookBeforeWrite(ctx context.Context, raw, path, expected string) ([]byte, error) {
	if r == nil || r.handbookFS == nil || !isHandbookPath(strings.TrimSpace(raw)) {
		return nil, nil
	}
	current, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		current = nil
	} else if err != nil {
		return nil, err
	}
	if expected != "" && handbookfs.Digest(current) != strings.TrimSpace(expected) {
		return nil, fmt.Errorf("handbook conflict: expected_digest does not match current file")
	}
	return current, nil
}
