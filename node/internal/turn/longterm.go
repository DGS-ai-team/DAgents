package turn

import (
	"context"
	"errors"
	"time"
)

// ErrLongTermVersionConflict 表示长期记忆在读取与写入之间已被其他操作修改。
var ErrLongTermVersionConflict = errors.New("long-term memory version conflict")

// LongTermSnapshot 为读取长期记忆时的内容与版本快照（用于乐观锁）。
type LongTermSnapshot struct {
	Content string
	Version time.Time
}

// LongTermStore 持久化 Agent 长期记忆（通常写入 agents.db）。
type LongTermStore interface {
	ReadLongTerm(ctx context.Context) (LongTermSnapshot, error)
	SaveLongTerm(ctx context.Context, content string, expectedVersion time.Time) error
}
