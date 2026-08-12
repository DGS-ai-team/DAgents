package processlock

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// configLockKey 从 config 绝对路径派生锁标识（与 Windows Mutex 后缀一致）。
func configLockKey(configPath string) (lockDir string, key string, err error) {
	abs, err := filepath.Abs(strings.TrimSpace(configPath))
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(strings.ToLower(abs)))
	return filepath.Dir(abs), hex.EncodeToString(sum[:8]), nil
}
