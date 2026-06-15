package hooks

import "time"

// PathStater 在 FS_ROOT 沙箱内查询相对路径是否存在及 mtime。
type PathStater interface {
	StatRelPath(relPath string) (exists bool, mtime time.Time, err error)
}
