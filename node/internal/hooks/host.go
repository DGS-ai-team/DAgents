package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// ErrHostNotAvailable 表示当前 RunPhase 未注入可用 Host。
var ErrHostNotAvailable = errors.New("hooks: host not available")

// ErrLLMQuotaExceeded 表示 turn 内 Hook LLM 调用超过配额。
var ErrLLMQuotaExceeded = errors.New("hooks: llm call quota exceeded")

// LoadedSkillInfo 为 Hook 可见的已加载 skill 摘要。
type LoadedSkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// RuntimeSummary 为 Hook 可见的运行时摘要。
type RuntimeSummary struct {
	ToolLoopCount int    `json:"tool_loop_count,omitempty"`
	FinishReason  string `json:"finish_reason,omitempty"`
	PendingHITL   bool   `json:"pending_hitl,omitempty"`
}

// FSPaths 为 Hook 可见的路径作用域（不含密钥）。
type FSPaths struct {
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	RuntimeRoot   string `json:"runtime_root,omitempty"`
	SkillsRoot    string `json:"skills_root,omitempty"`
}

// HostSnapshot 为 Host 只读快照。
type HostSnapshot struct {
	History      []llm.Message              `json:"history,omitempty"`
	SystemPrompt string                     `json:"system_prompt,omitempty"`
	LoadedSkills []LoadedSkillInfo          `json:"loaded_skills,omitempty"`
	Runtime      RuntimeSummary             `json:"runtime"`
	SessionStore map[string]json.RawMessage `json:"session_store,omitempty"`
	FSPaths      FSPaths                    `json:"fs_paths,omitempty"`
}

// LLMCompleteRequest 为 Hook 内二次 LLM 补全请求。
type LLMCompleteRequest struct {
	ReuseSystemPrompt bool
	UserPrompt        string
	MaxOutputTokens   int
}

// LLMCompleteResponse 为 Hook 内 LLM 补全结果。
type LLMCompleteResponse struct {
	Text string
}

// Host 为 in-process Hook 可调用的显式能力面（禁止反射）。
type Host interface {
	Snapshot() HostSnapshot
	SessionStoreGet(key string) (any, bool)
	SessionStoreSet(key string, val any) error
	SessionStoreDelete(key string) error
	LLMComplete(ctx context.Context, req LLMCompleteRequest) (LLMCompleteResponse, error)
}

type noopHost struct{}

// NoopHost 返回空实现 Host（测试或未注入 session Host 时使用）。
func NoopHost() Host { return noopHost{} }

func (noopHost) Snapshot() HostSnapshot { return HostSnapshot{} }

func (noopHost) SessionStoreGet(string) (any, bool) { return nil, false }

func (noopHost) SessionStoreSet(string, any) error { return nil }

func (noopHost) SessionStoreDelete(string) error { return nil }

func (noopHost) LLMComplete(context.Context, LLMCompleteRequest) (LLMCompleteResponse, error) {
	return LLMCompleteResponse{}, ErrHostNotAvailable
}

// WindowHistory 截取 history 最近 window 条；window <= 0 时不截断。
func WindowHistory(msgs []llm.Message, window int) []llm.Message {
	if window <= 0 || len(msgs) <= window {
		return append([]llm.Message(nil), msgs...)
	}
	return append([]llm.Message(nil), msgs[len(msgs)-window:]...)
}

// CloneSessionStore 深拷贝 session store map。
func CloneSessionStore(src map[string]json.RawMessage) map[string]json.RawMessage {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(src))
	for k, v := range src {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

// EnrichContext 从 Host 快照填充 Context 通用字段。
func EnrichContext(hc *Context, host Host) {
	if hc == nil || host == nil {
		return
	}
	snap := host.Snapshot()
	hc.History = append([]llm.Message(nil), snap.History...)
	hc.SystemPrompt = snap.SystemPrompt
	if len(snap.LoadedSkills) > 0 {
		hc.LoadedSkills = append([]LoadedSkillInfo(nil), snap.LoadedSkills...)
	}
	hc.Runtime = snap.Runtime
	if len(snap.SessionStore) > 0 {
		hc.SessionStore = CloneSessionStore(snap.SessionStore)
	}
	if snap.FSPaths != (FSPaths{}) {
		fp := snap.FSPaths
		hc.FSPaths = &fp
	}
}

// EncodeSessionStoreValue 将任意 JSON 可序列化值编码为 RawMessage。
func EncodeSessionStoreValue(val any) (json.RawMessage, error) {
	if val == nil {
		return json.RawMessage("null"), nil
	}
	switch v := val.(type) {
	case json.RawMessage:
		if !json.Valid(v) {
			return nil, fmt.Errorf("hooks: invalid json.RawMessage")
		}
		return append(json.RawMessage(nil), v...), nil
	case []byte:
		if !json.Valid(v) {
			return nil, fmt.Errorf("hooks: invalid json bytes")
		}
		return append(json.RawMessage(nil), v...), nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
}
