package api

import (
	"net/http"
	"runtime"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

func (s *Server) registerPolicyRoutes() {
	s.mux.HandleFunc("GET /v1/policy", s.handleGetPolicy)
	s.mux.HandleFunc("PUT /v1/policy/tools", s.handlePutToolPolicy)
	s.mux.HandleFunc("PUT /v1/policy/shell/{shell_type}", s.handlePutShellPolicy)
}

func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	policyDir := s.cfg.PolicyDir()
	engine, err := policy.LoadRuntime(s.cfg.RuntimeDir())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_load_failed", err.Error(), nil)
		return
	}
	snap, err := policy.LoadSnapshot(policyDir, engine, s.sessions.ToolNames())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_snapshot_failed", err.Error(), nil)
		return
	}
	snap.Platform.GOOS = runtime.GOOS
	defaultShell, _ := policy.ResolveShellType(nil)
	snap.Platform.DefaultShell = string(defaultShell)

	shellQuery := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("shell")))
	if shellQuery != "" && shellQuery != "auto" {
		st, err := policy.ParseShellTypeParam(shellQuery)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_shell", err.Error(), nil)
			return
		}
		key := string(st)
		filtered := snap.Shell[key]
		snap.Shell = map[string][]policy.ShellPolicyEntry{key: filtered}
	}
	writeJSON(w, http.StatusOK, snap)
}

type policyToolUpdatesBody struct {
	Updates []policy.ToolUpdate `json:"updates"`
}

func (s *Server) handlePutToolPolicy(w http.ResponseWriter, r *http.Request) {
	var body policyToolUpdatesBody
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if len(body.Updates) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_updates", "updates is required", nil)
		return
	}
	policyDir := s.cfg.PolicyDir()
	if err := policy.ApplyToolUpdates(policyDir, body.Updates); err != nil {
		writeAPIError(w, http.StatusBadRequest, "policy_update_failed", err.Error(), nil)
		return
	}
	if err := s.reloadPolicy(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_reload_failed", err.Error(), nil)
		return
	}
	s.logger.Info("policy tools updated", "count", len(body.Updates))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type policyShellUpdatesBody struct {
	Updates []policy.ShellUpdate `json:"updates"`
	Deletes []string             `json:"deletes"`
}

func (s *Server) handlePutShellPolicy(w http.ResponseWriter, r *http.Request) {
	shellRaw := strings.TrimSpace(r.PathValue("shell_type"))
	shellType, err := policy.ParseShellTypeParam(shellRaw)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_shell", err.Error(), nil)
		return
	}
	var body policyShellUpdatesBody
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if len(body.Updates) == 0 && len(body.Deletes) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_updates", "updates or deletes is required", nil)
		return
	}
	policyDir := s.cfg.PolicyDir()
	if err := policy.ApplyShellPolicyChanges(policyDir, shellType, body.Updates, body.Deletes); err != nil {
		writeAPIError(w, http.StatusBadRequest, "policy_update_failed", err.Error(), nil)
		return
	}
	if err := s.reloadPolicy(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "policy_reload_failed", err.Error(), nil)
		return
	}
	s.logger.Info("policy shell updated", "shell", shellType, "updates", len(body.Updates), "deletes", len(body.Deletes))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) reloadPolicy() error {
	engine, err := policy.LoadRuntime(s.cfg.RuntimeDir())
	if err != nil {
		return err
	}
	s.sessions.ReloadPolicy(engine)
	return nil
}
