package hooks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type httpHook struct {
	name    string
	url     string
	phases  []Phase
	client  *http.Client
	timeout time.Duration
	log     *slog.Logger
}

func newHTTPHook(entry ExternalHookEntry, logger *slog.Logger) Hook {
	url := strings.TrimSpace(entry.URL)
	if url == "" {
		return nil
	}
	timeout := entry.Timeout
	if timeout <= 0 {
		timeout = defaultHTTPHookTimeout
	}
	return &httpHook{
		name:    entry.Name,
		url:     url,
		phases:  append([]Phase(nil), entry.Phases...),
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
		log:     logger,
	}
}

func (h *httpHook) Name() string { return h.name }

func (h *httpHook) Phases() []Phase { return append([]Phase(nil), h.phases...) }

func (h *httpHook) Run(ctx context.Context, hc *Context) (Result, error) {
	if h == nil || hc == nil {
		return Result{Action: ActionContinue}, nil
	}
	body, err := marshalHookContext(hc)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode >= 500 {
		return Result{}, fmt.Errorf("hooks http %s returned %d", h.name, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		if h.log != nil {
			h.log.Warn("hooks http client error", "hook", h.name, "status", resp.StatusCode)
		}
		return Result{Action: ActionContinue}, nil
	}
	return parseExternalResult(raw)
}
