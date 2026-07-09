package shared

import (
	"context"
	"sync"
)

// TurnGate 用户 turn 栅栏（与 Python `SessionController` 对齐）。
//
// 职责：submit 后阻塞 Wait，直到语义 B 的 done 或 error 结束本轮。
// 边界：seq<=seqFence 的陈旧 SSE（如在途 trigger）忽略；HITL 暂停的 done 正常结束等待。
type TurnGate struct {
	mu          sync.Mutex
	awaiting    bool
	contentSeen bool
	seqFence    int
	lastSeq     int
	done        chan struct{}
}

// NewTurnGate 构造 turn 栅栏。
func NewTurnGate() *TurnGate {
	return &TurnGate{}
}

// Awaiting 是否正在等待本轮用户消息对应的 turn 结束。
func (g *TurnGate) Awaiting() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.awaiting
}

// LastSeq 返回已见到的最大 SSE 序号。
func (g *TurnGate) LastSeq() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastSeq
}

// NoteSeq 更新最大 SSE 序号；seq<=0 时忽略。
func (g *TurnGate) NoteSeq(seq int) {
	if seq <= 0 {
		return
	}
	g.mu.Lock()
	if seq > g.lastSeq {
		g.lastSeq = seq
	}
	g.mu.Unlock()
}

// BeginSubmit 在用户 submit 后调用：以当前 lastSeq 设栅栏并阻塞 Wait。
func (g *TurnGate) BeginSubmit() {
	g.beginTurnWait()
}

// BeginImplicitTurn 被动续跑（side_effect_continue 等）：等同 BeginSubmit 但不绑 user submit。
func (g *TurnGate) BeginImplicitTurn() {
	g.beginTurnWait()
}

func (g *TurnGate) beginTurnWait() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.awaiting = true
	g.contentSeen = false
	g.seqFence = g.lastSeq
	if g.done != nil {
		select {
		case <-g.done:
		default:
			close(g.done)
		}
	}
	g.done = make(chan struct{})
}

// IsStale 判断 replay/在途 turn 的陈旧事件（seq<=fence）。
func (g *TurnGate) IsStale(seq int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return seq > 0 && seq <= g.seqFence
}

// MarkTurnContent submit 后见到 assistant/tool 等内容事件时标记。
func (g *TurnGate) MarkTurnContent() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.awaiting {
		g.contentSeen = true
	}
}

// ShouldAcceptDone 是否应把本条 done 视为本轮 turn 边界。
func (g *TurnGate) ShouldAcceptDone(seq int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.awaiting {
		return false
	}
	return g.contentSeen || seq > g.seqFence
}

// FinishTurn 结束等待（语义 B done 或 error 路径）。
func (g *TurnGate) FinishTurn() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.awaiting {
		return
	}
	g.awaiting = false
	if g.done != nil {
		close(g.done)
		g.done = nil
	}
}

// Reset 切换 session 等场景清空栅栏状态。
func (g *TurnGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.awaiting = false
	g.contentSeen = false
	g.seqFence = 0
	if g.done != nil {
		close(g.done)
		g.done = nil
	}
}

// ApplyHydrateSeqHint 设置 SSE 去重水位（F-H5，对齐 Web applyHydrateSeqHint）。
func (g *TurnGate) ApplyHydrateSeqHint(seq int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if seq > 0 {
		g.lastSeq = seq
		g.seqFence = seq
	} else {
		g.seqFence = 0
	}
}

// SSEStartSeq 返回 hydrate 后 SSE 订阅起始 seq。
func (g *TurnGate) SSEStartSeq() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastSeq
}

// Wait 阻塞至 FinishTurn 或 ctx 取消。
func (g *TurnGate) Wait(ctx context.Context) error {
	g.mu.Lock()
	ch := g.done
	g.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}
