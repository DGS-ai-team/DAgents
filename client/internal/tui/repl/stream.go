package repl

import (
	"context"
	"fmt"
	"os"
	"strings"
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
	assistantLineOpen bool
	reasoningLineOpen bool

	turn *tuishared.TurnGate

	lifecycleSuppress *tuishared.ChildLifecycleSuppress
}

func newStreamRunner(
	client *nodeapi.Client,
	sessionID string,
	transcript *tuishared.Transcript,
	toolFold *tuishared.ToolFold,
	printMu *sync.Mutex,
	showReasoning *bool,
	turn *tuishared.TurnGate,
) *streamRunner {
	return &streamRunner{
		client:            client,
		sessionID:         sessionID,
		transcript:        transcript,
		toolFold:          toolFold,
		printMu:           printMu,
		showReasoning:     showReasoning,
		turn:              turn,
		lifecycleSuppress: tuishared.NewChildLifecycleSuppress(),
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
	r.turn.NoteSeq(ev.Seq)
	if r.turn.IsStale(ev.Seq) {
		return true, nil
	}
	if clihitl.ShouldSkipChildRuntimeDisplay(ev.Type, ev.Data) {
		return true, nil
	}
	switch ev.Type {
	case "assistant", "reasoning", "tool_call", "tool_result":
		r.turn.MarkTurnContent()
	}
	switch ev.Type {
	case "temporary_agent_created", "temporary_agent_completed", "temporary_agent_cancelled":
		childID := clihitl.ChildSessionIDFromData(ev.Data)
		if !r.lifecycleSuppress.ShouldSuppressLifecycle(childID, ev.Type) {
			if line := clihitl.FormatChildLifecycleLine(ev.Type, ev.Data); line != "" {
				r.logSystem(line)
			}
		}
		return true, nil
	}
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
			if eventType == "tool_call" {
				r.lifecycleSuppress.NoteToolCallEvent(data)
			} else if eventType == "tool_result" {
				name := strings.TrimSpace(fmt.Sprint(data["tool_name"]))
				if name == "" {
					name = strings.TrimSpace(fmt.Sprint(data["name"]))
				}
				content := strings.TrimSpace(fmt.Sprint(data["content"]))
				if content == "" {
					content = strings.TrimSpace(fmt.Sprint(data["output"]))
				}
				r.lifecycleSuppress.NoteToolResult(name, content)
			}
			for _, line := range tuishared.FormatToolEvent(eventType, data, r.toolFold.Verbose()) {
				r.logSystem(line)
			}
		},
		OnCompression: func(eventType string, data map[string]any) {
			r.logSystem(clihitl.FormatContextCompression(eventType, data))
		},
		OnError: func(msg string) {
			r.logSystem("error: " + msg)
		},
	}
	// REPL 模式 HITL 走 stdin；Interact 为 nil。主循环在 turn 期间不读 stdin。
	cont, err := clihitl.HandleStreamEvent(ctx, r.client, r.sessionID, ev, sink, nil, false)
	if ev.Type == "error" && r.turn.Awaiting() {
		r.turn.FinishTurn()
	}
	if ev.Type == "done" {
		r.finishAssistantLine()
		r.finishReasoningLine()
		if r.turn.ShouldAcceptDone(ev.Seq) {
			r.turn.FinishTurn()
		}
		return true, err
	}
	return cont, err
}

func (r *streamRunner) printAssistant(text string) {
	r.printMu.Lock()
	defer r.printMu.Unlock()
	if text != "" {
		r.assistantLineOpen = true
	}
	fmt.Print(text)
	r.transcript.AppendPartial("assistant", text)
}

func (r *streamRunner) finishAssistantLine() {
	r.printMu.Lock()
	defer r.printMu.Unlock()
	if r.assistantLineOpen {
		fmt.Println()
		r.assistantLineOpen = false
	}
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
