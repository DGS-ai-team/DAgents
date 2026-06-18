// Package hostsnapshot 提供进程级主机环境快照（启动时采集，供 system prompt 等读取）。
package hostsnapshot

import (
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Snapshot 为启动时刻采集的环境信息（不可变）。
type Snapshot struct {
	CapturedAtUnix  float64
	OSKind          string
	SysPlatform     string
	PlatformSystem  string
	PlatformRelease string
	Machine         string
	LoginName       string
	EffectiveUID    *int
	EffectiveGID    *int
}

var (
	mu     sync.RWMutex
	cached *Snapshot
)

// CaptureAtStartup 显式采集并缓存快照；Node 启动路径调用一次。
func CaptureAtStartup() Snapshot {
	s := buildSnapshot()
	mu.Lock()
	cached = &s
	mu.Unlock()
	return s
}

// Get 返回主机快照；尚未采集时惰性构建。
func Get() Snapshot {
	mu.RLock()
	if cached != nil {
		s := *cached
		mu.RUnlock()
		return s
	}
	mu.RUnlock()
	s := buildSnapshot()
	mu.Lock()
	if cached == nil {
		cached = &s
	} else {
		s = *cached
	}
	mu.Unlock()
	return s
}

func buildSnapshot() Snapshot {
	loginName := ""
	if u, err := user.Current(); err == nil {
		loginName = strings.TrimSpace(u.Username)
	}
	var euid, egid *int
	if runtime.GOOS != "windows" {
		if uid := os.Getuid(); uid >= 0 {
			v := uid
			euid = &v
		}
		if gid := os.Getgid(); gid >= 0 {
			v := gid
			egid = &v
		}
	}
	return Snapshot{
		CapturedAtUnix:  float64(time.Now().Unix()),
		OSKind:          inferOSKind(),
		SysPlatform:     runtime.GOOS,
		PlatformSystem:  runtime.GOOS,
		PlatformRelease: runtime.Version(),
		Machine:         runtime.GOARCH,
		LoginName:       loginName,
		EffectiveUID:    euid,
		EffectiveGID:    egid,
	}
}

func inferOSKind() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	default:
		return "other"
	}
}

// FormatEnvironmentSection 格式化为 system prompt「当前运行环境」正文。
func FormatEnvironmentSection(s Snapshot) string {
	loginDisplay := strings.TrimSpace(s.LoginName)
	if loginDisplay == "" {
		loginDisplay = "未知"
	}
	platformLine := "`" + s.SysPlatform + "` · " + s.PlatformSystem + " " + s.PlatformRelease + " · " + s.Machine
	lines := []string{
		"- 操作系统类别：`" + s.OSKind + "`",
		"- 平台摘要：" + platformLine,
		"- 当前进程用户（登录名）：`" + loginDisplay + "`",
	}
	if s.EffectiveUID != nil && s.EffectiveGID != nil {
		lines = append(lines, fmt.Sprintf("- 有效 UID / GID：`%d` / `%d`", *s.EffectiveUID, *s.EffectiveGID))
	} else {
		lines = append(lines, "- 有效 UID / GID：不适用（当前运行时非 POSIX 或未提供）")
	}
	return strings.Join(lines, "\n")
}
