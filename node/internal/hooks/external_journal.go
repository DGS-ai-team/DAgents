package hooks

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type journalHook struct {
	name   string
	path   string
	phases []Phase
	log    *slog.Logger
}

func newJournalHook(entry ExternalHookEntry, runtimeDir string, logger *slog.Logger) Hook {
	return &journalHook{
		name:   entry.Name,
		path:   journalPath(entry.JournalPath, runtimeDir),
		phases: append([]Phase(nil), entry.Phases...),
		log:    logger,
	}
}

func journalPath(configured, runtimeDir string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	base := strings.TrimSpace(runtimeDir)
	if base == "" {
		base = "."
	}
	return filepath.Join(base, "logs", "hooks.jsonl")
}

func (h *journalHook) Name() string { return h.name }

func (h *journalHook) Phases() []Phase { return append([]Phase(nil), h.phases...) }

func (h *journalHook) Run(_ context.Context, hc *Context) (Result, error) {
	if h == nil || hc == nil {
		return Result{Action: ActionContinue}, nil
	}
	record := map[string]any{
		"recorded_at": time.Now().UTC().Format(time.RFC3339Nano),
		"hook":        h.name,
		"context":     contextToDTO(hc),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		if h.log != nil {
			h.log.Warn("hooks journal serialize failed", "hook", h.name, "error", err)
		}
		return Result{Action: ActionContinue}, nil
	}
	if err := appendJournalLine(h.path, string(raw)+"\n"); err != nil {
		if h.log != nil {
			h.log.Warn("hooks journal write failed", "hook", h.name, "path", h.path, "error", err)
		}
	}
	return Result{Action: ActionContinue}, nil
}

func appendJournalLine(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}
