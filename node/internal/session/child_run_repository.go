package session

import (
	"context"
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// NewChildRunRepository 把 session 使用的 SQLite store 适配成 childagent
// 的持久化边界，避免 childagent 依赖 store→turn 的包链。
func NewChildRunRepository(st *store.SQLiteStore) childagent.RunRepository {
	if st == nil {
		return nil
	}
	return childRunRepository{store: st}
}

type childRunRepository struct {
	store *store.SQLiteStore
}

func (r childRunRepository) SaveChildRun(ctx context.Context, rec childagent.RunRecord) error {
	return r.store.SaveChildRun(ctx, store.ChildRunRecord{
		ChildAgentID: rec.ChildAgentID, ParentAgentID: rec.ParentAgentID,
		NodeID: rec.NodeID, ToolCallID: rec.ToolCallID, Purpose: rec.Purpose,
		Status: rec.Status, Phase: rec.Phase,
		AllowedTools: rec.AllowedTools, LoadedSkills: rec.LoadedSkills,
		Progress: json.RawMessage(rec.ProgressJSON), TurnCount: rec.TurnCount,
		MaxTurns: rec.MaxTurns, Summary: rec.Summary, Error: rec.Error,
		CreatedAt: rec.CreatedAt, ExpiresAt: rec.ExpiresAt,
		UpdatedAt: rec.UpdatedAt, FinishedAt: rec.FinishedAt,
		Revision: rec.Revision,
	})
}

func (r childRunRepository) ListChildRuns(ctx context.Context, parentID string, limit int) ([]childagent.RunRecord, error) {
	items, err := r.store.ListChildRuns(ctx, parentID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]childagent.RunRecord, 0, len(items))
	for _, item := range items {
		out = append(out, childagent.RunRecord{
			ChildAgentID: item.ChildAgentID, ParentAgentID: item.ParentAgentID,
			NodeID: item.NodeID, ToolCallID: item.ToolCallID, Purpose: item.Purpose,
			Status: item.Status, Phase: item.Phase,
			AllowedTools: item.AllowedTools, LoadedSkills: item.LoadedSkills,
			ProgressJSON: append([]byte(nil), item.Progress...), TurnCount: item.TurnCount,
			MaxTurns: item.MaxTurns, Summary: item.Summary, Error: item.Error,
			CreatedAt: item.CreatedAt, ExpiresAt: item.ExpiresAt,
			UpdatedAt: item.UpdatedAt, FinishedAt: item.FinishedAt,
			Revision: item.Revision,
		})
	}
	return out, nil
}

var _ childagent.RunRepository = childRunRepository{}
