package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/handbookfs"
)

// HandbookNoop records a handbook edit request whose resulting bytes were
// already present on disk. Path is relative to the handbook namespace.
type HandbookNoop struct {
	ToolCallID string
	ToolName   string
	Path       string
	Digest     string
}

type handbookNoopRecorderKey struct{}

func WithHandbookNoopRecorder(ctx context.Context, recorder func(context.Context, HandbookNoop) error) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, handbookNoopRecorderKey{}, recorder)
}

func handbookNoopRecorderFromContext(ctx context.Context) func(context.Context, HandbookNoop) error {
	if ctx == nil {
		return nil
	}
	recorder, _ := ctx.Value(handbookNoopRecorderKey{}).(func(context.Context, HandbookNoop) error)
	return recorder
}

func recordHandbookNoop(ctx context.Context, toolName, rawPath string, content []byte) error {
	recorder := handbookNoopRecorderFromContext(ctx)
	if recorder == nil || !handbookMaintenance(ctx) || !isHandbookPath(rawPath) {
		return nil
	}
	rawPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(rawPath)))
	path := strings.TrimPrefix(rawPath, "handbook/")
	if path == "" || path == "." || strings.HasPrefix(path, "../") || path == ".." {
		return fmt.Errorf("invalid handbook relative path")
	}
	return recorder(ctx, HandbookNoop{ToolCallID: toolCallIDFromContext(ctx), ToolName: toolName, Path: path, Digest: handbookfs.Digest(content)})
}
