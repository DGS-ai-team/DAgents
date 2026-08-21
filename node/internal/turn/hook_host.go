package turn

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

const defaultMaxHookLLMCalls = 2

// HookHostConfig 控制 session Host 行为。
type HookHostConfig struct {
	HistoryWindow int
	MaxLLMCalls   int
	RuntimeDir    string
	SkillsRoot    string
}

func (c HookHostConfig) normalized() HookHostConfig {
	out := c
	if out.MaxLLMCalls <= 0 {
		out.MaxLLMCalls = defaultMaxHookLLMCalls
	}
	return out
}

type hookHostState struct {
	mu sync.Mutex

	store map[string]json.RawMessage
	dirty bool

	history      []llm.Message
	stepIndex    int
	pendingHITL  bool
	finishReason string

	loadedSkills []skills.LoadedSkill
	systemPrompt string
	fsRoot       string

	llmCalls int
}

// sessionHookHost 实现 hooks.Host，供 RunPhase 注入。
type sessionHookHost struct {
	o         *Orchestrator
	sessionID string
	cfg       HookHostConfig
	state     *hookHostState
}

func (o *Orchestrator) newSessionHookHost(sessionID string, history []llm.Message, finishReason string) hooks.Host {
	if o == nil {
		return hooks.NoopHost()
	}
	o.ensureHookHostState()
	st := o.hookHostState
	st.mu.Lock()
	defer st.mu.Unlock()
	st.history = append([]llm.Message(nil), history...)
	st.finishReason = finishReason
	if o.skillAccess.Get != nil {
		st.loadedSkills = append([]skills.LoadedSkill(nil), o.skillAccess.Get()...)
	}
	st.systemPrompt = o.composeSystemPrompt(sessionID)
	st.fsRoot = o.fsRoot
	return &sessionHookHost{
		o:         o,
		sessionID: sessionID,
		cfg:       o.hookHostCfg.normalized(),
		state:     st,
	}
}

func (o *Orchestrator) ensureHookHostState() {
	if o.hookHostState == nil {
		o.hookHostState = &hookHostState{
			store: make(map[string]json.RawMessage),
		}
	}
}

// SetHookStore 恢复或初始化 session hook_store。
func (o *Orchestrator) SetHookStore(store map[string]json.RawMessage) {
	o.ensureHookHostState()
	o.hookHostState.mu.Lock()
	defer o.hookHostState.mu.Unlock()
	if len(store) == 0 {
		o.hookHostState.store = make(map[string]json.RawMessage)
		return
	}
	o.hookHostState.store = hooks.CloneSessionStore(store)
}

// HookStoreSnapshot 返回当前 hook_store 副本。
func (o *Orchestrator) HookStoreSnapshot() map[string]json.RawMessage {
	if o == nil || o.hookHostState == nil {
		return nil
	}
	o.hookHostState.mu.Lock()
	defer o.hookHostState.mu.Unlock()
	return hooks.CloneSessionStore(o.hookHostState.store)
}

// HookStoreDirty 表示 hook_store 是否有未持久化变更。
func (o *Orchestrator) HookStoreDirty() bool {
	if o == nil || o.hookHostState == nil {
		return false
	}
	o.hookHostState.mu.Lock()
	defer o.hookHostState.mu.Unlock()
	return o.hookHostState.dirty
}

// ClearHookStore 清空 session hook_store。
func (o *Orchestrator) ClearHookStore() {
	o.ensureHookHostState()
	o.hookHostState.mu.Lock()
	defer o.hookHostState.mu.Unlock()
	o.hookHostState.store = make(map[string]json.RawMessage)
	o.hookHostState.dirty = true
}

func (o *Orchestrator) setHookHostRuntime(stepIndex int, pending bool) {
	o.ensureHookHostState()
	o.hookHostState.mu.Lock()
	defer o.hookHostState.mu.Unlock()
	o.hookHostState.stepIndex = stepIndex
	o.hookHostState.pendingHITL = pending
}

// resetHookHostLLMQuota 在新 user turn 开始时清零 turn 内 Hook LLM 配额计数。
func (o *Orchestrator) resetHookHostLLMQuota() {
	o.ensureHookHostState()
	o.hookHostState.mu.Lock()
	o.hookHostState.llmCalls = 0
	o.hookHostState.mu.Unlock()
}

func (h *sessionHookHost) Snapshot() hooks.HostSnapshot {
	if h == nil || h.state == nil {
		return hooks.HostSnapshot{}
	}
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	var loaded []hooks.LoadedSkillInfo
	for _, sk := range h.state.loadedSkills {
		loaded = append(loaded, hooks.LoadedSkillInfo{
			Name:        sk.SkillName,
			Description: sk.Description,
		})
	}
	return hooks.HostSnapshot{
		History:      hooks.WindowHistory(h.state.history, h.cfg.HistoryWindow),
		SystemPrompt: h.state.systemPrompt,
		LoadedSkills: loaded,
		Runtime: hooks.RuntimeSummary{
			ToolLoopCount: h.state.stepIndex,
			FinishReason:  h.state.finishReason,
			PendingHITL:   h.state.pendingHITL,
		},
		SessionStore: hooks.CloneSessionStore(h.state.store),
		FSPaths: hooks.FSPaths{
			FSRoot:     h.state.fsRoot,
			RuntimeDir: h.cfg.RuntimeDir,
			SkillsRoot: h.cfg.SkillsRoot,
		},
	}
}

func (h *sessionHookHost) SessionStoreGet(key string) (any, bool) {
	if h == nil || h.state == nil || key == "" {
		return nil, false
	}
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	raw, ok := h.state.store[key]
	if !ok {
		return nil, false
	}
	var val any
	if err := json.Unmarshal(raw, &val); err != nil {
		return string(raw), true
	}
	return val, true
}

func (h *sessionHookHost) SessionStoreSet(key string, val any) error {
	if h == nil || h.state == nil || key == "" {
		return nil
	}
	raw, err := hooks.EncodeSessionStoreValue(val)
	if err != nil {
		return err
	}
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	if h.state.store == nil {
		h.state.store = make(map[string]json.RawMessage)
	}
	h.state.store[key] = raw
	h.state.dirty = true
	return nil
}

func (h *sessionHookHost) SessionStoreDelete(key string) error {
	if h == nil || h.state == nil || key == "" {
		return nil
	}
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	delete(h.state.store, key)
	h.state.dirty = true
	return nil
}

func (h *sessionHookHost) LLMComplete(ctx context.Context, req hooks.LLMCompleteRequest) (hooks.LLMCompleteResponse, error) {
	if h == nil || h.o == nil || h.o.llm == nil {
		return hooks.LLMCompleteResponse{}, hooks.ErrHostNotAvailable
	}
	h.state.mu.Lock()
	if h.state.llmCalls >= h.cfg.MaxLLMCalls {
		h.state.mu.Unlock()
		return hooks.LLMCompleteResponse{}, hooks.ErrLLMQuotaExceeded
	}
	h.state.llmCalls++
	systemPrompt := h.state.systemPrompt
	if !req.ReuseSystemPrompt {
		systemPrompt = ""
	}
	h.state.mu.Unlock()

	text, err := h.o.llm.CompleteText(ctx, llm.CompleteRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   req.UserPrompt,
	})
	if err != nil {
		return hooks.LLMCompleteResponse{}, err
	}
	return hooks.LLMCompleteResponse{Text: text}, nil
}

func (o *Orchestrator) applyHookPhaseEffects(sessionID string, history *[]llm.Message, hc *hooks.Context) {
	if o == nil || hc == nil {
		return
	}
	if history != nil && len(hc.History) == len(*history)+1 {
		if idx, msg, ok := findSingleHistoryInsert(*history, hc.History); ok {
			o.insertHistory(sessionID, history, idx, msg)
		} else if historyIsPrefix(*history, hc.History) {
			o.appendHistory(sessionID, history, hc.History[len(*history)])
		}
	} else if history != nil && len(hc.History) > len(*history) && historyIsPrefix(*history, hc.History) {
		// history_append：仅在 hc.History 为旧 history 前缀扩展时追加，并走 journal。
		for i := len(*history); i < len(hc.History); i++ {
			o.appendHistory(sessionID, history, hc.History[i])
		}
	}
	if len(hc.SessionStore) > 0 && o.hookHostState != nil {
		o.hookHostState.mu.Lock()
		if o.hookHostState.store == nil {
			o.hookHostState.store = make(map[string]json.RawMessage)
		}
		for k, v := range hc.SessionStore {
			o.hookHostState.store[k] = append(json.RawMessage(nil), v...)
		}
		o.hookHostState.dirty = true
		o.hookHostState.mu.Unlock()
	}
}

// historyIsPrefix 判断 prefix 是否为 full 的前缀（按 Role/Content/Name 浅比较）。
func historyIsPrefix(prefix, full []llm.Message) bool {
	if len(prefix) > len(full) {
		return false
	}
	for i := range prefix {
		if !historyMessageEqual(prefix[i], full[i]) {
			return false
		}
	}
	return true
}

// findSingleHistoryInsert 若 full 相对 old 仅多插入一条消息，返回插入下标与消息。
func findSingleHistoryInsert(old, full []llm.Message) (int, llm.Message, bool) {
	if len(full) != len(old)+1 {
		return 0, llm.Message{}, false
	}
	for i := 0; i < len(full); i++ {
		if i == len(old) {
			return i, full[i], true
		}
		if !historyMessageEqual(old[i], full[i]) {
			if !historyIsPrefix(old[i:], full[i+1:]) {
				return 0, llm.Message{}, false
			}
			return i, full[i], true
		}
	}
	return 0, llm.Message{}, false
}

func historyMessageEqual(a, b llm.Message) bool {
	return a.Role == b.Role && a.Content == b.Content && a.Name == b.Name
}

func (o *Orchestrator) runPhase(ctx context.Context, phase hooks.Phase, hc *hooks.Context, sessionID string, history *[]llm.Message, finishReason string) (hooks.Context, error) {
	if o == nil || o.toolHooks == nil {
		if hc == nil {
			return hooks.Context{}, nil
		}
		out := *hc
		return out, nil
	}
	var hist []llm.Message
	if history != nil {
		hist = *history
	}
	host := o.newSessionHookHost(sessionID, hist, finishReason)
	out, err := o.toolHooks.RunPhase(ctx, phase, hc, host)
	if err != nil {
		return out, err
	}
	o.applyHookPhaseEffects(sessionID, history, &out)
	return out, nil
}
