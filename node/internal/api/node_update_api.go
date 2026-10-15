package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/DGS-ai-team/DAgents/shared/update"
)

const (
	nodeUpdatePackageMaxBytes = int64(1 << 30)
	nodeUpdatePackageTimeout  = 10 * time.Minute
	nodeUpdateStatusMaxAge    = 15 * time.Minute
)

func (s *Server) handleNodeUpdatePackage(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeAPIError(w, http.StatusForbidden, "local_only", "Node 更新包仅允许本机下载", nil)
		return
	}
	if r.URL.RawQuery != "" || r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_update_package_request", "更新包下载不接受 query 或 request body", nil)
		return
	}
	if s.updateChecker == nil || s.cfg == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "update_unavailable", "Node 更新检查不可用", nil)
		return
	}
	status := s.updateChecker.Snapshot()
	if !status.ManageReachable || !status.UpdateEnabled || !status.UpgradeAvailable {
		writeAPIError(w, http.StatusConflict, "update_unavailable", "当前没有可下载的更新包", nil)
		return
	}
	checkedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(status.LastCheckedAt))
	if err != nil || time.Since(checkedAt) < -time.Minute || time.Since(checkedAt) > nodeUpdateStatusMaxAge {
		writeAPIError(w, http.StatusConflict, "update_check_expired", "更新检查已过期，请先重新读取 Node 更新状态", nil)
		return
	}
	asset := status.Asset
	if asset == nil {
		writeAPIError(w, http.StatusBadGateway, "update_asset_unavailable", "更新检查没有返回安装包候选", nil)
		return
	}
	assetURL, _ := asset["download_url"].(string)
	filename, _ := asset["filename"].(string)
	expectedSHA, _ := asset["sha256"].(string)
	expectedSize, ok := assetSizeBytes(asset["size_bytes"])
	if !ok || expectedSize <= 0 {
		writeAPIError(w, http.StatusBadGateway, "update_asset_invalid", "更新包缺少有效的 size_bytes 元数据", nil)
		return
	}
	if expectedSize > nodeUpdatePackageMaxBytes {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "update_package_too_large", "更新包超过 Node 下载大小限制", nil)
		return
	}
	expectedSHA = strings.ToLower(strings.TrimSpace(expectedSHA))
	if len(expectedSHA) != sha256.Size*2 {
		writeAPIError(w, http.StatusBadGateway, "update_asset_invalid", "更新包缺少有效的 SHA-256 校验值", nil)
		return
	}
	if _, err := hex.DecodeString(expectedSHA); err != nil {
		writeAPIError(w, http.StatusBadGateway, "update_asset_invalid", "更新包 SHA-256 校验值格式无效", nil)
		return
	}
	if strings.TrimSpace(filename) == "" {
		writeAPIError(w, http.StatusBadGateway, "update_asset_invalid", "更新包缺少文件名", nil)
		return
	}
	if hasControlCharacters(filename) || hasControlCharacters(status.LatestVersion) || strings.TrimSpace(status.LatestVersion) == "" {
		writeAPIError(w, http.StatusBadGateway, "update_asset_invalid", "更新包候选信息无效", nil)
		return
	}
	remoteURL, err := validatedManageAssetURL(s.cfg.Manage.URL, assetURL)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "update_asset_invalid", "更新包地址不在配置的 Manage 服务来源内", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), nodeUpdatePackageTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL.String(), nil)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "update_download_failed", "无法构造更新包请求", nil)
		return
	}
	request.Header.Set(update.AgentIDHeader, strings.TrimSpace(s.cfg.NodeID))
	if token := strings.TrimSpace(s.cfg.Manage.NodeToken); token != "" {
		request.Header.Set(update.TokenHeader, token)
	}
	client := &http.Client{
		Timeout: nodeUpdatePackageTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// Never forward the Node credential through an upstream redirect.
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "update_download_failed", "从 Manage 下载更新包失败", nil)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		writeAPIError(w, http.StatusBadGateway, "update_download_failed", "Manage 未返回完整的更新包", map[string]any{"upstream_status": response.StatusCode})
		return
	}
	if response.ContentLength >= 0 && response.ContentLength != expectedSize {
		writeAPIError(w, http.StatusBadGateway, "update_package_incomplete", "Manage 返回的更新包大小与候选信息不一致", nil)
		return
	}
	if headerSHA := strings.ToLower(strings.TrimSpace(response.Header.Get("X-Release-Sha256"))); headerSHA != "" && headerSHA != expectedSHA {
		writeAPIError(w, http.StatusBadGateway, "update_package_integrity", "Manage 返回的 SHA-256 与更新候选不一致", nil)
		return
	}
	if headerVersion := strings.TrimSpace(response.Header.Get("X-Release-Version")); headerVersion != "" && headerVersion != strings.TrimSpace(status.LatestVersion) {
		writeAPIError(w, http.StatusBadGateway, "update_package_integrity", "Manage 返回的版本与更新候选不一致", nil)
		return
	}

	tempDir, err := os.MkdirTemp("", "dagents-node-update-")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "update_temp_unavailable", "Node 无法准备更新包临时空间", nil)
		return
	}
	defer os.RemoveAll(tempDir)
	tempFile, err := os.CreateTemp(tempDir, "package-*")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "update_temp_unavailable", "Node 无法准备更新包临时文件", nil)
		return
	}
	tempPath := tempFile.Name()
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tempFile, hasher), io.LimitReader(response.Body, expectedSize+1))
	closeErr := tempFile.Close()
	if copyErr != nil || closeErr != nil {
		writeAPIError(w, http.StatusBadGateway, "update_package_incomplete", "更新包下载中断", nil)
		return
	}
	if written > nodeUpdatePackageMaxBytes {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "update_package_too_large", "更新包超过 Node 下载大小限制", nil)
		return
	}
	if written != expectedSize {
		writeAPIError(w, http.StatusBadGateway, "update_package_incomplete", "更新包下载大小与候选信息不一致", nil)
		return
	}
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if actualSHA != expectedSHA {
		writeAPIError(w, http.StatusBadGateway, "update_package_integrity", "更新包 SHA-256 校验失败", nil)
		return
	}
	file, err := os.Open(tempPath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "update_temp_unavailable", "Node 无法读取已校验的更新包", nil)
		return
	}
	defer file.Close()
	safeName := filepath.Base(strings.ReplaceAll(strings.ReplaceAll(filename, "\\", "/"), "\x00", ""))
	if safeName == "." || safeName == string(filepath.Separator) || safeName == "" {
		safeName = "dagents-update-package"
	}
	contentDisposition := mime.FormatMediaType("attachment", map[string]string{"filename": safeName})
	if contentDisposition == "" {
		writeAPIError(w, http.StatusBadGateway, "update_asset_invalid", "更新包文件名无法安全编码", nil)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", contentDisposition)
	w.Header().Set("Content-Length", strconv.FormatInt(written, 10))
	w.Header().Set("X-Node-Package-Version", status.LatestVersion)
	w.Header().Set("X-Node-Package-SHA256", actualSHA)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func hasControlCharacters(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func isLoopbackRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func assetSizeBytes(value any) (int64, bool) {
	switch n := value.(type) {
	case float64:
		if n < 0 || n > float64(^uint64(0)>>1) || n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	case int64:
		return n, n >= 0
	case int:
		return int64(n), n >= 0
	case json.Number:
		i, err := n.Int64()
		return i, err == nil && i >= 0
	default:
		return 0, false
	}
}

func validatedManageAssetURL(manageBase, assetURL string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(manageBase))
	if err != nil || !validHTTPOrigin(base) || base.User != nil || base.Fragment != "" {
		return nil, fmt.Errorf("invalid Manage base URL")
	}
	asset, err := url.Parse(strings.TrimSpace(assetURL))
	if err != nil || strings.TrimSpace(assetURL) == "" {
		return nil, fmt.Errorf("invalid asset URL")
	}
	resolved := base.ResolveReference(asset)
	if !validHTTPOrigin(resolved) || resolved.User != nil || resolved.Fragment != "" || !sameHTTPOrigin(base, resolved) {
		return nil, fmt.Errorf("asset URL is outside Manage origin")
	}
	return resolved, nil
}

func validHTTPOrigin(u *url.URL) bool {
	if u == nil || u.Hostname() == "" {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

func sameHTTPOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}
