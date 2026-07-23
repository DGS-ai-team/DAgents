package logfiles

import (
	"fmt"
	"path/filepath"
	"time"
)

const dateLayout = "2006-01-02"

// DatedName 返回按日区分的日志文件名。
// 例：prefix=node → node-2026-07-23.log / node-2026-07-23.err.log
func DatedName(prefix string, errLog bool, day time.Time) string {
	if day.IsZero() {
		day = time.Now()
	}
	d := day.Format(dateLayout)
	if errLog {
		return fmt.Sprintf("%s-%s.err.log", prefix, d)
	}
	return fmt.Sprintf("%s-%s.log", prefix, d)
}

// JoinDated 在 dir 下拼接按日日志路径。
func JoinDated(dir, prefix string, errLog bool, day time.Time) string {
	return filepath.Join(dir, DatedName(prefix, errLog, day))
}
