// Package update 实现 Local Assistant 版本检查与安装包下载。
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

const (
	// ExitUpToDate 表示当前已是最新版本，无需下载。
	ExitUpToDate = 3

	tokenHeader   = "x-dagents-a2a-token"
	agentIDHeader = "x-dagents-agent-id"
)

// Options 控制 update 子命令行为。
type Options struct {
	CheckOnly bool
	Force     bool
	Output    string
}

// Run 查询 Node /v1/agent/update，可选下载安装包到 Output。
func Run(ctx context.Context, cfg *config.Config, opt Options) int {
	client := nodeapi.New(cfg.Local.Endpoint, &http.Client{Timeout: 30 * time.Second})
	status, err := client.GetAgentUpdate(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check failed: %v\n", err)
		return 1
	}
	printStatus(status)
	if opt.CheckOnly {
		return 0
	}
	if !status.UpgradeAvailable {
		return ExitUpToDate
	}
	if strings.TrimSpace(opt.Output) == "" {
		fmt.Fprintln(os.Stderr, "update: --output path is required")
		return 1
	}
	if !opt.Force {
		ok, err := confirm(fmt.Sprintf("升级到 %s？", status.LatestVersion))
		if err != nil {
			fmt.Fprintf(os.Stderr, "confirm failed: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Println("已取消")
			return 1
		}
	}
	if err := downloadPackage(ctx, cfg, status, opt.Output); err != nil {
		fmt.Fprintf(os.Stderr, "download failed: %v\n", err)
		return 1
	}
	fmt.Printf("已下载更新包: %s\n", opt.Output)
	return 0
}

func printStatus(status *nodeapi.AgentUpdateStatus) {
	fmt.Printf("当前版本: %s\n", status.CurrentVersion)
	fmt.Printf("最新版本: %s\n", status.LatestVersion)
	fmt.Printf("平台: %s  渠道: %s\n", status.Platform, status.Channel)
	if status.ManageReachable {
		fmt.Printf("Manage: 可达\n")
	} else {
		fmt.Printf("Manage: 不可达\n")
	}
	if msg := strings.TrimSpace(status.Message); msg != "" {
		fmt.Println(msg)
	}
	if notes := strings.TrimSpace(status.ReleaseNotes); notes != "" {
		fmt.Println("Release notes:")
		fmt.Println(notes)
	}
	if status.UpgradeAvailable {
		cmd := strings.TrimSpace(status.ApplyCommand)
		if cmd == "" {
			cmd = "dagents update"
		}
		fmt.Printf("升级命令: %s\n", cmd)
	}
}

func confirm(prompt string) (bool, error) {
	fmt.Printf("%s [y/N] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}

func downloadPackage(ctx context.Context, cfg *config.Config, status *nodeapi.AgentUpdateStatus, destPath string) error {
	if status.Asset == nil {
		return fmt.Errorf("update response missing asset")
	}
	downloadURL, _ := status.Asset["download_url"].(string)
	downloadURL = strings.TrimSpace(downloadURL)
	if downloadURL == "" {
		return fmt.Errorf("update response missing download_url")
	}
	expectedSHA, _ := status.Asset["sha256"].(string)
	expectedSHA = strings.ToLower(strings.TrimSpace(expectedSHA))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	if id := strings.TrimSpace(cfg.AgentID); id != "" {
		req.Header.Set(agentIDHeader, id)
	}
	if token := strings.TrimSpace(cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}

	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
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
