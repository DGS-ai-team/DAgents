package hooks

import "time"

// PathStater 在 Agent workspace 内查询相对路径是否存在及 mtime。
type PathStater interface {
	StatRelPath(relPath string) (exists bool, mtime time.Time, err error)
}
