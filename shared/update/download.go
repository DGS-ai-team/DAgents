package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DownloadRequest 控制安装包下载与 sha256 校验。
type DownloadRequest struct {
	URL            string
	DestPath       string
	ExpectedSHA256 string
	AgentID        string
	NodeToken      string
	Client         *http.Client
}

// DownloadPackage 下载安装包到 DestPath，可选校验 sha256。
func DownloadPackage(ctx context.Context, req DownloadRequest) error {
	downloadURL := strings.TrimSpace(req.URL)
	if downloadURL == "" {
		return fmt.Errorf("download url is empty")
	}
	destPath := strings.TrimSpace(req.DestPath)
	if destPath == "" {
		return fmt.Errorf("dest path is empty")
	}
	expectedSHA := strings.ToLower(strings.TrimSpace(req.ExpectedSHA256))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	if id := strings.TrimSpace(req.AgentID); id != "" {
		httpReq.Header.Set(AgentIDHeader, id)
	}
	if token := strings.TrimSpace(req.NodeToken); token != "" {
		httpReq.Header.Set(TokenHeader, token)
	}

	client := req.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Minute}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("download HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	tmpPath := destPath + ".part"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)
	if _, err := io.Copy(writer, resp.Body); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if expectedSHA != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if got != expectedSHA {
			os.Remove(tmpPath)
			return fmt.Errorf("sha256 mismatch: expected %s, got %s", expectedSHA, got)
		}
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
