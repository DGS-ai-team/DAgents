package tools

import (
	"os"
	"time"
)

// StatRelPath 在 Agent workspace 内 Stat 相对路径（供 Agent 文件信任链 Hook 使用）。
func (r *Registry) StatRelPath(relPath string) (exists bool, mtime time.Time, err error) {
	if r == nil {
		return false, time.Time{}, os.ErrInvalid
	}
	abs, err := r.resolvePath(relPath)
	if err != nil {
		return false, time.Time{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return false, time.Time{}, nil
		}
		return false, time.Time{}, err
	}
	if info.IsDir() {
		return false, time.Time{}, nil
	}
	return true, info.ModTime(), nil
}
