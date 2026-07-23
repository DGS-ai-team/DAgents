package nodectl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/logfiles"
)

const EnvHome = "DAGENTS_HOME"

// Layout 描述安装根目录下的关键路径。
type Layout struct {
	Home       string
	ConfigPath string
	NodeExe    string
	PidFile    string
	LogOut      string
	LogErr      string
	ShellLog    string
	ShellLogErr string
}

// ResolveLayout 解析安装根与 Node 相关路径。
//
// 查找顺序：DAGENTS_HOME → 可执行文件在 bin/ 下时取其父目录 → 当前工作目录。
func ResolveLayout(configPath string) (*Layout, error) {
	home, err := resolveHome()
	if err != nil {
		return nil, err
	}
	cfg := strings.TrimSpace(configPath)
	if cfg == "" {
		return nil, fmt.Errorf("config path is empty")
	}
	if !filepath.IsAbs(cfg) {
		cfg = filepath.Join(home, cfg)
	}
	cfg, err = filepath.Abs(cfg)
	if err != nil {
		return nil, err
	}
	nodeExe := filepath.Join(home, "bin", "dagents-node.exe")
	runtimeDir := filepath.Join(home, ".runtime")
	logDir := filepath.Join(runtimeDir, "logs")
	now := time.Now()
	return &Layout{
		Home:        home,
		ConfigPath:  cfg,
		NodeExe:     nodeExe,
		PidFile:     filepath.Join(runtimeDir, "node.pid"),
		LogOut:      logfiles.JoinDated(logDir, "node", false, now),
		LogErr:      logfiles.JoinDated(logDir, "node", true, now),
		ShellLog:    logfiles.JoinDated(logDir, "shell", false, now),
		ShellLogErr: logfiles.JoinDated(logDir, "shell", true, now),
	}, nil
}

func resolveHome() (string, error) {
	if h := strings.TrimSpace(os.Getenv(EnvHome)); h != "" {
		return filepath.Abs(h)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir, err := filepath.Abs(filepath.Dir(exe))
	if err != nil {
		return "", err
	}
	if strings.EqualFold(filepath.Base(dir), "bin") {
		return filepath.Dir(dir), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return wd, nil
}
