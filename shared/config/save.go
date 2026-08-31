package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SaveFile 将配置写入 YAML 文件（原子替换；不保留原文件注释）。
// 兼容旧路径：仍可写出完整 Config。新流程请优先 SaveBootstrapFile + node_settings.db。
func SaveFile(path string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty config path")
	}
	work := *cfg
	work.SyncActiveProfileFromFlat()
	work.ApplyDefaults()
	if err := work.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	out, err := yaml.Marshal(&work)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return writeAtomic(path, out)
}

// BootstrapConfig 为进程引导 YAML：仅 listen / local（运行时根固定 DefaultRuntimeRoot）。
type BootstrapConfig struct {
	Listen ListenConfig `yaml:"listen"`
	Local  LocalConfig  `yaml:"local"`
}

// SaveBootstrapFile 仅写入 listen/local，供 Node 引导；其余设置在 node_settings.db。
func SaveBootstrapFile(path string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty config path")
	}
	boot := BootstrapConfig{
		Listen: cfg.Listen,
		Local:  cfg.Local,
	}
	if strings.TrimSpace(boot.Listen.Host) == "" {
		boot.Listen.Host = DefaultListenHost
	}
	if boot.Listen.Port == 0 {
		boot.Listen.Port = DefaultListenPort
	}
	if strings.TrimSpace(boot.Local.Endpoint) == "" {
		boot.Local.Endpoint = fmt.Sprintf("http://%s:%d", boot.Listen.Host, boot.Listen.Port)
	}
	out, err := yaml.Marshal(&boot)
	if err != nil {
		return fmt.Errorf("marshal bootstrap config: %w", err)
	}
	header := []byte("# DAgents Node bootstrap（仅 listen/local）\n# 其余设置保存在 ./.runtime/node_settings.db 与 llm_configs.db，请用 Web UI 修改。\n")
	return writeAtomic(path, append(header, out...))
}

// FileHasMigratableSettings 判断引导 YAML 是否仍含 listen/local 以外的顶层键（胖配置，应迁入 SQLite）。
// 必须读原始文件：LoadFile+ApplyDefaults 会注入 llm.profiles 等，无法用 Config 结构体可靠判断。
func FileHasMigratableSettings(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false
	}
	for k := range raw {
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "listen", "local":
			continue
		default:
			return true
		}
	}
	return false
}

func writeAtomic(path string, out []byte) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
