package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// BootstrapNodeSettings 打开 node_settings.db：空库时写入产品默认，再 overlay 到 cfg。
// YAML 只负责 listen/local 引导；运行时设置的唯一持久化来源是 node_settings.db。
func BootstrapNodeSettings(ctx context.Context, cfg *config.Config, configPath string, logger *slog.Logger) (*NodeSettingsStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	if logger == nil {
		logger = slog.Default()
	}
	s, err := OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		return nil, err
	}
	empty, err := s.Empty(ctx)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	if empty {
		seed := ProductNodeSettingsSeed()
		// 保留引导层已解析的 node_id；runtime_root 写死为 DefaultRuntimeRoot。
		seed.NodeID = cfg.NodeID
		logger.Info("seeding product node settings", "path", cfg.NodeSettingsDBPath())
		if err := s.Save(ctx, seed); err != nil {
			_ = s.Close()
			return nil, err
		}
		if configPath != "" {
			if err := config.SaveBootstrapFile(configPath, cfg); err != nil {
				logger.Warn("slim bootstrap YAML failed", "path", configPath, "error", err)
			}
		}
	}
	snap, err := s.Load(ctx)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	if snap != nil {
		OverlayNodeSettings(cfg, snap)
		cfg.ApplyDefaults()
		if err := cfg.Validate(); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("node settings invalid: %w", err)
		}
	}
	return s, nil
}
