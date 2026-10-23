package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SaveFile 将配置写入 YAML 文件（原子替换；不保留原文件注释）。
// 该 API 供独立 Client 与无持久化的测试使用；Node 运行时设置写入 node_settings.db。
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

// BootstrapConfig 为 Node 进程引导 YAML：仅包含监听地址。
type BootstrapConfig struct {
	Listen ListenConfig `yaml:"listen"`
}

// SaveBootstrapFile 仅写入 Node 监听配置；客户端 endpoint 等配置由客户端自己管理。
func SaveBootstrapFile(path string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty config path")
	}
	boot := BootstrapConfig{Listen: cfg.Listen}
	if strings.TrimSpace(boot.Listen.Host) == "" {
		boot.Listen.Host = DefaultListenHost
	}
	if boot.Listen.Port == 0 {
		boot.Listen.Port = DefaultListenPort
	}
	out, err := yaml.Marshal(&boot)
	if err != nil {
		return fmt.Errorf("marshal bootstrap config: %w", err)
	}
	header := []byte("# DAgents Node bootstrap（仅 listen）\n# 其余设置保存在 ./.runtime/node_settings.db 与 llm_configs.db，请用 DAgents Desktop 修改。\n")
	return writeAtomic(path, append(header, out...))
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
