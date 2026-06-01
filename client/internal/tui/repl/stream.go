package repl

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

const reconnectDelay = 5 * time.Second

// streamRunner 在后台维持 SSE 订阅，断线后按 Last-Event-ID 重连。
type streamRunner struct {
	client    *nodeapi.Client
	sessionID string

	mu           sync.Mutex
	lastEventSeq int

	transcript *tuishared.Transcript
	toolFold   *tuishared.ToolFold
	printMu    *sync.Mutex
	showReasoning *bool
	reasoningLineOpen bool

	onTurnDone func()
}

func newStreamRunner(
	client *nodeapi.Client,
	sessionID string,
	transcript *tuishared.Transcript,
	toolFold *tuishared.ToolFold,
	printMu *sync.Mutex,
	showReasoning *bool,
	onTurnDone func(),
) *streamRunner {
	return &streamRunner{
		client:        client,
		sessionID:     sessionID,
		transcript:    transcript,
		toolFold:      toolFold,
		printMu:       printMu,
		showReasoning: showReasoning,
		onTurnDone:    onTurnDone,
	}
}

func (r *streamRunner) lastSeq() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastEventSeq
}

func (r *streamRunner) noteSeq(seq int) {
	if seq <= 0 {
		return
	}
	r.mu.Lock()
	if seq > r.lastEventSeq {
		r.lastEventSeq = seq
	}
	r.mu.Unlock()
}

// Run 阻塞直到 ctx 取消；断线时自动重连。
func (r *streamRunner) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fromSeq := r.lastSeq()
		err := r.client.StreamEvents(ctx, r.sessionID, fromSeq, func(ev nodeapi.StreamEvent) bool {
			r.noteSeq(ev.Seq)
			cont, handleErr := r.handleEvent(ctx, ev)
			if handleErr != nil {
				r.logSystem(fmt.Sprintf("事件处理失败: %v", handleErr))
			}
			return cont
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			continue
		}
		r.logSystem(fmt.Sprintf("SSE 断开 (%v)，%s 后重连…", err, reconnectDelay))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectDelay):
		}
	}
}

func (r *streamRunner) handleEvent(ctx context.Context, ev nodeapi.StreamEvent) (bool, error) {
	sink := clihitl.Sink{
		OnAssistant: func(text string) {
			r.printAssistant(text)
		},
		OnReasoning: func(text string) {
			if r.showReasoning != nil && *r.showReasoning {
				r.printReasoning(text)
			}
		},
		OnTool: func(eventType string, data map[string]any) {
			r.finishAssistantLine()
			r.finishReasoningLine()
			line := r.toolFold.Format(eventType, data)
			r.logSystem(line)
		},
		OnCompression: func(eventType string, data map[string]any) {
			r.logSystem(clihitl.FormatContextCompression(eventType, data))
		},
		OnError: func(msg string) {
			r.logSystem("error: " + msg)
		},
	}
	// REPL 模式 HITL 走 stdin；Interact 为 nil。
	cont, err := clihitl.HandleStreamEvent(ctx, r.client, r.sessionID, ev, sink, nil, false)
	if !cont && ev.Type == "done" {
		r.finishAssistantLine()
		r.finishReasoningLine()
		if r.onTurnDone != nil {
			r.onTurnDone()
		}
		return true, err
	}
	return cont, err
}

func (r *streamRunner) printAssistant(text string) {
	r.printMu.Lock()
	defer r.printMu.Unlock()
	fmt.Print(text)
	r.transcript.AppendPartial("assistant", text)
}

func (r *streamRunner) finishAssistantLine() {
	r.printMu.Lock()
	defer r.printMu.Unlock()
	fmt.Println()
	r.transcript.FinishPartial("assistant")
}

func (r *streamRunner) printReasoning(text string) {
	r.printMu.Lock()
	defer r.printMu.Unlock()
	if !r.reasoningLineOpen {
		fmt.Fprintf(os.Stderr, "[reasoning] ")
		r.reasoningLineOpen = true
	}
	fmt.Fprint(os.Stderr, text)
	r.transcript.AppendPartial("reasoning", text)
}

func (r *streamRunner) finishReasoningLine() {
	r.printMu.Lock()
	defer r.printMu.Unlock()
	if r.reasoningLineOpen {
		fmt.Fprintln(os.Stderr)
		r.reasoningLineOpen = false
	}
	r.transcript.FinishPartial("reasoning")
}

func (r *streamRunner) logSystem(line string) {
	r.printMu.Lock()
	defer r.printMu.Unlock()
	fmt.Fprintln(os.Stderr, line)
	r.transcript.Add("[system] " + line)
}
