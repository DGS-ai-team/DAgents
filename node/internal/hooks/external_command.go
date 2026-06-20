package hooks

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type commandHook struct {
	name         string
	command      []string
	allowedPaths []string
	phases       []Phase
	log          *slog.Logger
}

func newCommandHook(entry ExternalHookEntry, logger *slog.Logger) Hook {
	if len(entry.Command) == 0 {
		return nil
	}
	if len(entry.AllowedPaths) == 0 {
		return nil
	}
	if !commandPathAllowed(entry.Command[0], entry.AllowedPaths) {
		return nil
	}
	return &commandHook{
		name:         entry.Name,
		command:      append([]string(nil), entry.Command...),
		allowedPaths: append([]string(nil), entry.AllowedPaths...),
		phases:       append([]Phase(nil), entry.Phases...),
		log:          logger,
	}
}

func commandPathAllowed(exe string, allowedRoots []string) bool {
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return false
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return false
	}
	for _, root := range allowedRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if abs == rootAbs || strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func (h *commandHook) Name() string { return h.name }

func (h *commandHook) Phases() []Phase { return append([]Phase(nil), h.phases...) }

func (h *commandHook) Run(ctx context.Context, hc *Context) (Result, error) {
	if h == nil || hc == nil {
		return Result{Action: ActionContinue}, nil
	}
	if !commandPathAllowed(h.command[0], h.allowedPaths) {
		return Result{}, fmt.Errorf("hooks command %q not in allowed_paths", h.name)
	}
	body, err := marshalHookContext(hc)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, h.command[0], h.command[1:]...)
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if h.log != nil && stderr.Len() > 0 {
			h.log.Warn("hooks command failed", "hook", h.name, "stderr", stderr.String(), "error", err)
		}
		return Result{}, err
	}
	return parseExternalResult(stdout.Bytes())
}
