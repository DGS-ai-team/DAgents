package config

import (
	"fmt"
	"os"
	"strings"
)

const EnvConfigPath = "DAGENTS_CONFIG"

// defaultConfigCandidates 为未显式指定 -config 时的查找顺序（相对当前工作目录）。
var defaultConfigCandidates = []string{
	"packaging/agent-client/config.yaml",
	"packaging/agent-client/config.example.yaml",
	"config.yaml",
}

// ResolveConfigPath 解析配置文件路径。

// 逻辑：
// 1. 显式 `-config` 非空则直接使用；
// 2. 否则读 `DAGENTS_CONFIG` 环境变量；
// 3. 否则按 defaultConfigCandidates 依次探测存在性；
// 4. 均未命中则返回 error。
//
// 异常：最终路径不存在时返回 error。
func ResolveConfigPath(explicit string) (string, error) {
	if p := strings.TrimSpace(explicit); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("config %q: %w", p, err)
		}
		return p, nil
	}
	if p := strings.TrimSpace(os.Getenv(EnvConfigPath)); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("%s %q: %w", EnvConfigPath, p, err)
		}
		return p, nil
	}
	for _, rel := range defaultConfigCandidates {
		if _, err := os.Stat(rel); err == nil {
			return rel, nil
		}
	}
	return "", fmt.Errorf(
		"config not found: pass -config <path> or set %s (tried: %s)",
		EnvConfigPath,
		strings.Join(defaultConfigCandidates, ", "),
	)
}
