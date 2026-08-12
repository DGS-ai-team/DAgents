package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// PackageUploadResult 为 Node 向 Manage 上传制品的结果摘要。
type PackageUploadResult struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// PackageUploader 通过 Manage admin API 上传 skill / externaltool / plugin 包。
type PackageUploader struct {
	cfg    *config.Config
	logger *slog.Logger
	client *http.Client
}

// NewPackageUploader 构造 Manage 制品上传客户端。
func NewPackageUploader(cfg *config.Config, logger *slog.Logger) *PackageUploader {
	if logger == nil {
		logger = slog.Default()
	}
	return &PackageUploader{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Enabled 表示 Manage 上传是否可用。
func (u *PackageUploader) Enabled() bool {
	return u != nil && u.cfg != nil && u.cfg.Manage.Enabled && strings.TrimSpace(u.cfg.Manage.URL) != ""
}

type uploadMeta struct {
	kind       string
	endpoint   string
	idField    string
	idValue    string
	version    string
	name       string
	platform   string
	publishURL string
}

// UploadSkill 上传 skill zip 至 Manage（可选 publish）。
func (u *PackageUploader) UploadSkill(ctx context.Context, localPath, skillID, version, name string, publish bool) (*PackageUploadResult, error) {
	return u.upload(ctx, localPath, uploadMeta{
		kind:     "skill",
		endpoint: "/v1/skills/packages",
		idField:  "skill_id",
		idValue:  skillID,
		version:  version,
		name:     name,
		publishURL: fmt.Sprintf("/v1/skills/packages/%s/versions/%s/publish",
			urlPathEscape(skillID), urlPathEscape(version)),
	}, publish)
}

// UploadExternalTool 上传外置 CLI / 二进制至 Manage。
func (u *PackageUploader) UploadExternalTool(ctx context.Context, localPath, toolID, version, name, platform string, publish bool) (*PackageUploadResult, error) {
	if strings.TrimSpace(platform) == "" {
		platform = "any"
	}
	return u.upload(ctx, localPath, uploadMeta{
		kind:     "externaltool",
		endpoint: "/v1/externaltools/packages",
		idField:  "tool_id",
		idValue:  toolID,
		version:  version,
		name:     name,
		platform: platform,
		publishURL: fmt.Sprintf("/v1/externaltools/packages/%s/versions/%s/publish",
			urlPathEscape(toolID), urlPathEscape(version)),
	}, publish)
}

// UploadPlugin 上传 Hook plugin .so 至 Manage。
func (u *PackageUploader) UploadPlugin(ctx context.Context, localPath, pluginID, version, name, platform string, publish bool) (*PackageUploadResult, error) {
	if strings.TrimSpace(platform) == "" {
		platform = "any"
	}
	return u.upload(ctx, localPath, uploadMeta{
		kind:     "plugin",
		endpoint: "/v1/plugins/packages",
		idField:  "plugin_id",
		idValue:  pluginID,
		version:  version,
		name:     name,
		platform: platform,
		publishURL: fmt.Sprintf("/v1/plugins/packages/%s/versions/%s/publish",
			urlPathEscape(pluginID), urlPathEscape(version)),
	}, publish)
}

func (u *PackageUploader) upload(ctx context.Context, localPath string, meta uploadMeta, publish bool) (*PackageUploadResult, error) {
	if !u.Enabled() {
		return nil, fmt.Errorf("manage 未启用，无法上传 %s", meta.kind)
	}
	abs, err := u.resolvePath(localPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("file is empty: %s", abs)
	}
	filename := filepath.Base(abs)
	status, err := u.postMultipart(ctx, meta, filename, data)
	if err != nil {
		return nil, err
	}
	out := &PackageUploadResult{
		Kind:    meta.kind,
		ID:      meta.idValue,
		Version: meta.version,
		Name:    meta.name,
		Status:  status,
	}
	if publish {
		if err := u.postEmpty(ctx, meta.publishURL); err != nil {
			return out, fmt.Errorf("upload ok but publish failed: %w", err)
		}
		out.Status = "published"
		out.Message = "uploaded and published"
	}
	return out, nil
}

func (u *PackageUploader) resolvePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	root, err := resolveFSRoot(u.cfg.FSRoot)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(raw) {
		abs, err := filepath.Abs(filepath.Clean(raw))
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	clean := filepath.Clean(raw)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes fs_root: %s", raw)
	}
	full := filepath.Join(root, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		return "", fmt.Errorf("path escapes fs_root: %s", raw)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", abs)
	}
	return abs, nil
}

func resolveFSRoot(fsRoot string) (string, error) {
	root := strings.TrimSpace(fsRoot)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("fs_root empty and getwd failed: %w", err)
		}
		root = wd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create fs_root: %w", err)
	}
	return abs, nil
}

func (u *PackageUploader) postMultipart(ctx context.Context, meta uploadMeta, filename string, data []byte) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField(meta.idField, meta.idValue)
	_ = w.WriteField("version", meta.version)
	_ = w.WriteField("name", meta.name)
	_ = w.WriteField("risk_level", "low")
	if meta.platform != "" {
		_ = w.WriteField("platform", meta.platform)
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	endpoint, err := u.manageURL(meta.endpoint)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	u.setAuthHeaders(req)
	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("manage upload %s: HTTP %d: %s", meta.kind, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err == nil {
		if st, ok := out["status"].(string); ok && st != "" {
			return st, nil
		}
	}
	return "draft", nil
}

func (u *PackageUploader) postEmpty(ctx context.Context, path string) error {
	endpoint, err := u.manageURL(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	u.setAuthHeaders(req)
	resp, err := u.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (u *PackageUploader) manageURL(path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(u.cfg.Manage.URL), "/")
	if base == "" {
		return "", fmt.Errorf("manage.url is empty")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path, nil
}

func (u *PackageUploader) setAuthHeaders(req *http.Request) {
	if token := strings.TrimSpace(u.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}
	if id := strings.TrimSpace(u.cfg.NodeID); id != "" {
		req.Header.Set(agentIDHeader, id)
	}
}

func urlPathEscape(s string) string {
	return strings.ReplaceAll(s, " ", "%20")
}
