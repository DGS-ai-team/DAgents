package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SaveFile 将配置写入 YAML 文件（原子替换；不保留原文件注释）。
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
