//go:build windows

package notify

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/pending"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/webui"
	toast "github.com/go-toast/toast"
)

const toastAppID = "DAgents Shell"

// Notifier 按 Agent 发送/更新 Windows Toast（F-N1/N2）。
type Notifier struct {
	endpoint string
	iconPath string
	mu       sync.Mutex
	last     map[string]pending.Entry
}

// New 构造 Toast 通知器；iconBytes 写入临时 ico 供 Toast 使用（可为 nil）。
func New(endpoint string, iconBytes []byte) *Notifier {
	n := &Notifier{
		endpoint: endpoint,
		last:     make(map[string]pending.Entry),
	}
	if len(iconBytes) > 0 {
		if path, err := writeTempIcon(iconBytes); err == nil {
			n.iconPath = path
		}
	}
	return n
}

// Sync 根据待办表发送或更新 Toast。
func (n *Notifier) Sync(entries []pending.Entry) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()

	current := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		current[e.SessionID] = struct{}{}
		prev := n.last[e.SessionID]
		if prev.Active() && prev.HITLItems == e.HITLItems && prev.HasUnread == e.HasUnread {
			continue
		}
		if err := n.push(e); err != nil {
			log.Printf("shell toast: %v", err)
		}
		n.last[e.SessionID] = e
	}
	for id := range n.last {
		if _, ok := current[id]; !ok {
			delete(n.last, id)
		}
	}
}

func (n *Notifier) push(e pending.Entry) error {
	title := "DAgents 待处理"
	message := e.SummaryLabel()
	id := e.AgentID
	if id == "" {
		id = e.SessionID
	}
	target := webui.AgentURL(n.endpoint, id)

	notification := toast.Notification{
		AppID:               toastAppID,
		Title:               title,
		Message:             message,
		ActivationType:      "protocol",
		ActivationArguments: target,
		Actions: []toast.Action{
			{Type: "protocol", Label: "打开", Arguments: target},
		},
	}
	if n.iconPath != "" {
		notification.Icon = n.iconPath
	}
	return notification.Push()
}

func writeTempIcon(data []byte) (string, error) {
	dir, err := os.MkdirTemp("", "dagents-shell-icon-*")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "icon.ico")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ToastTitle 返回 Agent 待办的 Toast 标题（测试用）。
func ToastTitle(e pending.Entry) string {
	return fmt.Sprintf("%s: %s", toastAppID, e.SummaryLabel())
}
