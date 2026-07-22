package turn

import "context"

// LongTermStore 持久化 Agent 长期记忆（通常写入 agents.db）。
type LongTermStore interface {
	ReadLongTerm(ctx context.Context) (string, error)
	SaveLongTerm(ctx context.Context, content string) error
}
