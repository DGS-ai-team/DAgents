package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type linuxChannelRequest struct {
	ChannelID        string `json:"channel_id"`
	DisplayName      string `json:"display_name"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Username         string `json:"username"`
	CredentialID     string `json:"credential_id"`
	HostKeyPolicy    string `json:"host_key_policy"`
	HostKeyRef       string `json:"host_key_ref"`
	RemoteShell      string `json:"remote_shell"`
	DefaultCWD       string `json:"default_cwd"`
	ConnectTimeoutMS int    `json:"connect_timeout_ms"`
	CommandTimeoutMS int    `json:"command_timeout_ms"`
	KeepaliveSeconds int    `json:"keepalive_seconds"`
	MaxSessions      int    `json:"max_sessions"`
	Enabled          *bool  `json:"enabled"`
}

type linuxCredentialRequest struct {
	CredentialID string  `json:"credential_id"`
	DisplayName  string  `json:"display_name"`
	AuthType     string  `json:"auth_type"`
	SecretRef    string  `json:"secret_ref"`
	SecretValue  *string `json:"secret_value"`
	UsernameHint string  `json:"username_hint"`
	Enabled      *bool   `json:"enabled"`
}

type linuxBindingRequest struct {
	ChannelID       string   `json:"channel_id"`
	Enabled         *bool    `json:"enabled"`
	IsDefault       bool     `json:"is_default"`
	RemoteCWD       string   `json:"remote_cwd"`
	Shell           string   `json:"shell"`
	MaxConcurrency  int      `json:"max_concurrency"`
	ApprovalMode    string   `json:"approval_mode"`
	AllowedCommands []string `json:"allowed_commands"`
	DeniedCommands  []string `json:"denied_commands"`
}

func (s *Server) registerLinuxChannelRoutes() {
	s.mux.HandleFunc("GET /v1/linux/channels", s.handleListLinuxChannels)
	s.mux.HandleFunc("POST /v1/linux/channels", s.handleCreateLinuxChannel)
	s.mux.HandleFunc("PATCH /v1/linux/channels/{channel_id}", s.handlePatchLinuxChannel)
	s.mux.HandleFunc("DELETE /v1/linux/channels/{channel_id}", s.handleDeleteLinuxChannel)
	s.mux.HandleFunc("POST /v1/linux/channels/{channel_id}/test", s.handleTestLinuxChannel)
	s.mux.HandleFunc("GET /v1/linux/credentials", s.handleListLinuxCredentials)
	s.mux.HandleFunc("POST /v1/linux/credentials", s.handleCreateLinuxCredential)
	s.mux.HandleFunc("PATCH /v1/linux/credentials/{credential_id}", s.handlePatchLinuxCredential)
	s.mux.HandleFunc("DELETE /v1/linux/credentials/{credential_id}", s.handleDeleteLinuxCredential)
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/linux-channels", s.handleGetAgentLinuxChannels)
	s.mux.HandleFunc("PUT /v1/agents/{agent_id}/linux-channels", s.handlePutAgentLinuxChannels)
}

func (s *Server) handleListLinuxChannels(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	items, err := s.linuxChannels.ListChannels(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "linux_channels_load_failed", err.Error(), nil)
		return
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		views = append(views, linuxChannelView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": views})
}

func (s *Server) handleCreateLinuxChannel(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	var req linuxChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	id, err := s.linuxChannels.GenerateChannelID(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "linux_channel_id_failed", err.Error(), nil)
		return
	}
	rec := linuxChannelRecordFromRequest(req, id)
	if err := s.linuxChannels.SaveChannel(r.Context(), rec); err != nil {
		writeAPIError(w, http.StatusBadRequest, "linux_channel_save_failed", err.Error(), nil)
		return
	}
	view, _ := s.linuxChannels.GetChannel(r.Context(), rec.ChannelID)
	if view == nil {
		writeAPIError(w, http.StatusInternalServerError, "linux_channel_load_failed", "saved channel could not be loaded", nil)
		return
	}
	writeJSON(w, http.StatusCreated, linuxChannelView(*view))
}

func (s *Server) handlePatchLinuxChannel(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("channel_id"))
	existing, err := s.linuxChannels.GetChannel(r.Context(), id)
	if err != nil || existing == nil {
		writeAPIError(w, http.StatusNotFound, "linux_channel_not_found", "Linux channel not found", nil)
		return
	}
	var req linuxChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	rec := linuxChannelRecordFromRequest(req, id)
	mergeLinuxChannelRecord(&rec, *existing, req)
	if err := s.linuxChannels.SaveChannel(r.Context(), rec); err != nil {
		writeAPIError(w, http.StatusBadRequest, "linux_channel_save_failed", err.Error(), nil)
		return
	}
	view, _ := s.linuxChannels.GetChannel(r.Context(), id)
	if view == nil {
		writeAPIError(w, http.StatusInternalServerError, "linux_channel_load_failed", "saved channel could not be loaded", nil)
		return
	}
	writeJSON(w, http.StatusOK, linuxChannelView(*view))
}

func (s *Server) handleDeleteLinuxChannel(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("channel_id"))
	if err := s.linuxChannels.DeleteChannel(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusNotFound, "linux_channel_not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleTestLinuxChannel(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil || s.linuxProvider == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel provider is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("channel_id"))
	status, err := s.linuxProvider.Test(r.Context(), tools.ExecutionTarget{Kind: "linux_channel", ID: id})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "linux_channel_test_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel_id": id, "available": status.Available, "message": status.Message})
}

func (s *Server) handleListLinuxCredentials(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	items, err := s.linuxChannels.ListCredentials(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "linux_credentials_load_failed", err.Error(), nil)
		return
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		views = append(views, linuxCredentialView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": views})
}

func (s *Server) handleCreateLinuxCredential(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	var req linuxCredentialRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	id, err := s.linuxChannels.GenerateCredentialID(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "linux_credential_id_failed", err.Error(), nil)
		return
	}
	rec, err := linuxCredentialRecordFromRequest(req, id)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "linux_credential_save_failed", err.Error(), nil)
		return
	}
	if err := s.linuxChannels.SaveCredential(r.Context(), rec); err != nil {
		writeAPIError(w, http.StatusBadRequest, "linux_credential_save_failed", err.Error(), nil)
		return
	}
	view, err := s.linuxChannels.GetCredential(r.Context(), rec.CredentialID)
	if err != nil || view == nil {
		writeAPIError(w, http.StatusInternalServerError, "linux_credential_load_failed", "saved credential could not be loaded", nil)
		return
	}
	writeJSON(w, http.StatusCreated, linuxCredentialView(*view))
}

func (s *Server) handlePatchLinuxCredential(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("credential_id"))
	existing, err := s.linuxChannels.GetCredential(r.Context(), id)
	if err != nil || existing == nil {
		writeAPIError(w, http.StatusNotFound, "linux_credential_not_found", "Linux credential not found", nil)
		return
	}
	var req linuxCredentialRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	if req.AuthType == "" {
		req.AuthType = existing.AuthType
	}
	rec, err := linuxCredentialRecordFromRequest(req, id)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "linux_credential_save_failed", err.Error(), nil)
		return
	}
	if rec.DisplayName == "" {
		rec.DisplayName = existing.DisplayName
	}
	if rec.AuthType == "" {
		rec.AuthType = existing.AuthType
	}
	if rec.SecretRef == "" {
		rec.SecretRef = existing.SecretRef
	}
	if req.Enabled == nil {
		rec.Enabled = existing.Enabled
	}
	if rec.UsernameHint == "" {
		rec.UsernameHint = existing.UsernameHint
	}
	if err := s.linuxChannels.SaveCredential(r.Context(), rec); err != nil {
		writeAPIError(w, http.StatusBadRequest, "linux_credential_save_failed", err.Error(), nil)
		return
	}
	view, err := s.linuxChannels.GetCredential(r.Context(), id)
	if err != nil || view == nil {
		writeAPIError(w, http.StatusInternalServerError, "linux_credential_load_failed", "saved credential could not be loaded", nil)
		return
	}
	writeJSON(w, http.StatusOK, linuxCredentialView(*view))
}

func (s *Server) handleDeleteLinuxCredential(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("credential_id"))
	if err := s.linuxChannels.DeleteCredential(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusNotFound, "linux_credential_not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleGetAgentLinuxChannels(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	items, err := s.linuxChannels.ListBindings(r.Context(), agentID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "linux_bindings_load_failed", err.Error(), nil)
		return
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		views = append(views, linuxBindingView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": agentID, "bindings": views})
}

func (s *Server) handlePutAgentLinuxChannels(w http.ResponseWriter, r *http.Request) {
	if s.linuxChannels == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "linux_channels_unavailable", "Linux channel store is not configured", nil)
		return
	}
	agentID := strings.TrimSpace(r.PathValue("agent_id"))
	var body struct {
		Bindings []linuxBindingRequest `json:"bindings"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	records := make([]store.LinuxChannelBindingRecord, 0, len(body.Bindings))
	for _, req := range body.Bindings {
		channelID := strings.TrimSpace(req.ChannelID)
		if channelID == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_linux_binding", "channel_id is required", nil)
			return
		}
		channel, err := s.linuxChannels.GetChannel(r.Context(), channelID)
		if err != nil || channel == nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_linux_binding", fmt.Sprintf("linux channel %q not found", channelID), nil)
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		records = append(records, store.LinuxChannelBindingRecord{
			AgentID: agentID, ChannelID: channelID, Enabled: enabled, IsDefault: req.IsDefault,
			RemoteCWD: req.RemoteCWD, Shell: req.Shell, MaxConcurrency: req.MaxConcurrency,
			ApprovalMode: req.ApprovalMode, AllowedCommands: req.AllowedCommands,
			DeniedCommands: req.DeniedCommands,
		})
	}
	if err := s.linuxChannels.ReplaceBindings(r.Context(), agentID, records); err != nil {
		writeAPIError(w, http.StatusBadRequest, "linux_binding_save_failed", err.Error(), nil)
		return
	}
	if s.agents != nil {
		if rec, err := s.agents.Get(r.Context(), agentID); err == nil && rec != nil {
			_ = s.ensureAgentRuntimeOpts(r.Context(), agentID, true)
		}
	}
	items, _ := s.linuxChannels.ListBindings(r.Context(), agentID)
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		views = append(views, linuxBindingView(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": agentID, "bindings": views})
}

func linuxChannelRecordFromRequest(req linuxChannelRequest, fallbackID string) store.LinuxChannelRecord {
	id := strings.TrimSpace(fallbackID)
	if id == "" {
		id = strings.TrimSpace(req.ChannelID)
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return store.LinuxChannelRecord{
		ChannelID: id, DisplayName: strings.TrimSpace(req.DisplayName), Host: strings.TrimSpace(req.Host),
		Port: req.Port, Username: strings.TrimSpace(req.Username), CredentialID: strings.TrimSpace(req.CredentialID),
		HostKeyPolicy: strings.TrimSpace(req.HostKeyPolicy), HostKeyRef: strings.TrimSpace(req.HostKeyRef),
		RemoteShell: strings.TrimSpace(req.RemoteShell), DefaultCWD: strings.TrimSpace(req.DefaultCWD),
		ConnectTimeoutMS: req.ConnectTimeoutMS, CommandTimeoutMS: req.CommandTimeoutMS,
		KeepaliveSeconds: req.KeepaliveSeconds, MaxSessions: req.MaxSessions, Enabled: enabled,
	}
}

func mergeLinuxChannelRecord(dst *store.LinuxChannelRecord, old store.LinuxChannelRecord, req linuxChannelRequest) {
	if dst.DisplayName == "" {
		dst.DisplayName = old.DisplayName
	}
	if dst.Host == "" {
		dst.Host = old.Host
	}
	if dst.Port == 0 {
		dst.Port = old.Port
	}
	if dst.Username == "" {
		dst.Username = old.Username
	}
	if dst.CredentialID == "" {
		dst.CredentialID = old.CredentialID
	}
	if dst.HostKeyPolicy == "" {
		dst.HostKeyPolicy = old.HostKeyPolicy
	}
	if dst.HostKeyRef == "" {
		dst.HostKeyRef = old.HostKeyRef
	}
	if dst.RemoteShell == "" {
		dst.RemoteShell = old.RemoteShell
	}
	if dst.DefaultCWD == "" {
		dst.DefaultCWD = old.DefaultCWD
	}
	if dst.ConnectTimeoutMS == 0 {
		dst.ConnectTimeoutMS = old.ConnectTimeoutMS
	}
	if dst.CommandTimeoutMS == 0 {
		dst.CommandTimeoutMS = old.CommandTimeoutMS
	}
	if dst.KeepaliveSeconds == 0 {
		dst.KeepaliveSeconds = old.KeepaliveSeconds
	}
	if dst.MaxSessions == 0 {
		dst.MaxSessions = old.MaxSessions
	}
	if req.Enabled == nil {
		dst.Enabled = old.Enabled
	}
}

func linuxCredentialRecordFromRequest(req linuxCredentialRequest, fallbackID string) (store.LinuxCredentialRecord, error) {
	id := strings.TrimSpace(fallbackID)
	if id == "" {
		id = strings.TrimSpace(req.CredentialID)
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	authType := strings.ToLower(strings.TrimSpace(req.AuthType))
	secretRef := strings.TrimSpace(req.SecretRef)
	if req.SecretValue != nil {
		if authType != "password" {
			return store.LinuxCredentialRecord{}, fmt.Errorf("direct secret input is supported for password credentials only")
		}
		if secretRef != "" {
			return store.LinuxCredentialRecord{}, fmt.Errorf("secret_ref and secret_value cannot both be provided")
		}
		if strings.TrimSpace(*req.SecretValue) == "" {
			return store.LinuxCredentialRecord{}, fmt.Errorf("secret_value is required for direct password credentials")
		}
		secretRef = encodeLinuxLiteralSecret(*req.SecretValue)
	}
	return store.LinuxCredentialRecord{CredentialID: id, DisplayName: strings.TrimSpace(req.DisplayName), AuthType: authType, SecretRef: secretRef, UsernameHint: strings.TrimSpace(req.UsernameHint), Enabled: enabled}, nil
}

func linuxCredentialView(rec store.LinuxCredentialRecord) map[string]any {
	return map[string]any{
		"credential_id": rec.CredentialID, "display_name": rec.DisplayName, "auth_type": rec.AuthType,
		"username_hint": rec.UsernameHint, "enabled": rec.Enabled,
		"has_secret": strings.TrimSpace(rec.SecretRef) != "", "secret_source": linuxSecretSource(rec.SecretRef),
	}
}

func linuxChannelView(rec store.LinuxChannelRecord) map[string]any {
	return map[string]any{
		"channel_id": rec.ChannelID, "display_name": rec.DisplayName, "host": rec.Host,
		"port": rec.Port, "username": rec.Username, "credential_id": rec.CredentialID,
		"host_key_policy": rec.HostKeyPolicy, "host_key_ref": rec.HostKeyRef,
		"remote_shell": rec.RemoteShell, "default_cwd": rec.DefaultCWD,
		"connect_timeout_ms": rec.ConnectTimeoutMS, "command_timeout_ms": rec.CommandTimeoutMS,
		"keepalive_seconds": rec.KeepaliveSeconds, "max_sessions": rec.MaxSessions,
		"enabled": rec.Enabled, "created_at": rec.CreatedAt, "updated_at": rec.UpdatedAt,
	}
}

func linuxBindingView(rec store.LinuxChannelBindingRecord) map[string]any {
	return map[string]any{
		"agent_id": rec.AgentID, "channel_id": rec.ChannelID, "enabled": rec.Enabled,
		"is_default": rec.IsDefault, "remote_cwd": rec.RemoteCWD, "shell": rec.Shell,
		"max_concurrency": rec.MaxConcurrency, "approval_mode": rec.ApprovalMode,
		"allowed_commands": rec.AllowedCommands, "denied_commands": rec.DeniedCommands,
		"created_at": rec.CreatedAt, "updated_at": rec.UpdatedAt,
	}
}

func resolveLinuxSecret(_ context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "literal:") {
		encoded := strings.TrimPrefix(ref, "literal:")
		if encoded == "" {
			return "", fmt.Errorf("linux literal secret is empty")
		}
		value, err := base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return "", fmt.Errorf("linux literal secret is invalid: %w", err)
		}
		if len(value) == 0 {
			return "", fmt.Errorf("linux literal secret is empty")
		}
		return string(value), nil
	}
	if !strings.HasPrefix(ref, "env:") {
		return "", fmt.Errorf("unsupported linux secret reference; use env:NAME or direct password input")
	}
	name := strings.TrimSpace(strings.TrimPrefix(ref, "env:"))
	if name == "" {
		return "", fmt.Errorf("linux secret environment name is empty")
	}
	value := os.Getenv(name)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("linux secret environment %q is empty", name)
	}
	return value, nil
}

func encodeLinuxLiteralSecret(value string) string {
	return "literal:" + base64.RawStdEncoding.EncodeToString([]byte(value))
}

func linuxSecretSource(ref string) string {
	ref = strings.TrimSpace(ref)
	switch {
	case strings.HasPrefix(ref, "env:"):
		return "environment"
	case strings.HasPrefix(ref, "literal:"):
		return "direct"
	case ref != "":
		return "legacy"
	default:
		return "none"
	}
}
