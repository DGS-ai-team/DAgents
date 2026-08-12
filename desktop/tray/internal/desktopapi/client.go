package desktopapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	shellupdate "github.com/DGS-ai-team/DAgents/desktop/tray/internal/update"
	sharedupdate "github.com/DGS-ai-team/DAgents/shared/update"
)

const exitShellUnavailable = 2

// BaseURL 返回 Shell localhost API 基址。
func BaseURL() string {
	return "http://" + DefaultListenAddr
}

// Health 探测 Shell desktop API 是否在线。
func Health(ctx context.Context, client *http.Client) error {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL()+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("desktop API health: status %d", resp.StatusCode)
	}
	return nil
}

// RunUpdateCommand 通过运行中 Shell 的 localhost API 执行 update（F-I12）。
func RunUpdateCommand(ctx context.Context, args []string) int {
	checkOnly, force := parseUpdateArgs(args)
	if err := Health(ctx, nil); err != nil {
		return exitShellUnavailable
	}
	if checkOnly {
		status, code := getUpdateStatus(ctx)
		printUpdateStatus(os.Stdout, status)
		return code
	}
	return postUpdateApply(ctx, force)
}

type applyRequest struct {
	Force bool `json:"force"`
}

type applyResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code,omitempty"`
	Status  sharedupdate.Status `json:"status,omitempty"`
}

func getUpdateStatus(ctx context.Context) (sharedupdate.Status, int) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL()+"/v1/desktop/update", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check: %v\n", err)
		return sharedupdate.Status{}, 1
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check: %v\n", err)
		return sharedupdate.Status{}, 1
	}
	defer resp.Body.Close()
	var status sharedupdate.Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		fmt.Fprintf(os.Stderr, "update check decode: %v\n", err)
		return sharedupdate.Status{}, 1
	}
	if !status.ManageReachable {
		return status, 1
	}
	if !status.UpgradeAvailable {
		return status, shellupdate.ExitUpToDate
	}
	return status, 0
}

func postUpdateApply(ctx context.Context, force bool) int {
	body, _ := json.Marshal(applyRequest{Force: force})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL()+"/v1/desktop/update/apply", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "update apply: %v\n", err)
		return 1
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update apply: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update apply read: %v\n", err)
		return 1
	}
	var out applyResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			fmt.Fprintf(os.Stderr, "update apply decode: %v\n", err)
			return 1
		}
	}
	if out.Message != "" {
		if out.OK {
			fmt.Fprintln(os.Stdout, out.Message)
		} else {
			fmt.Fprintln(os.Stderr, out.Message)
		}
	}
	if out.Code != 0 {
		return out.Code
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0
	}
	if resp.StatusCode == http.StatusConflict {
		return shellupdate.ExitUpToDate
	}
	if len(raw) > 0 && out.Message == "" {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(string(raw)))
	}
	return 1
}

func parseUpdateArgs(args []string) (checkOnly, force bool) {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--check":
			checkOnly = true
		case "--force":
			force = true
		}
	}
	return checkOnly, force
}

func printUpdateStatus(w io.Writer, status sharedupdate.Status) {
	fmt.Fprintf(w, "当前版本: %s\n", status.CurrentVersion)
	fmt.Fprintf(w, "最新版本: %s\n", status.LatestVersion)
	fmt.Fprintf(w, "平台: %s  渠道: %s\n", status.Platform, status.Channel)
	if status.ManageReachable {
		fmt.Fprintln(w, "Manage: 可达")
	} else {
		fmt.Fprintln(w, "Manage: 不可达")
	}
	if msg := strings.TrimSpace(status.Message); msg != "" {
		fmt.Fprintln(w, msg)
	}
}
