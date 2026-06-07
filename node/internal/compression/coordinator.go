package compression

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

const summarySystemPrompt = `你是会话压缩助手。你会基于给定消息块生成结构化摘要，必须严格包含以下四段并使用中文：
1) 任务目标
2) 重要结论
3) 修改过的文件和资源
4) 下一步动作
要求：不要编造不存在的信息；文件/资源尽量用路径或明确名称；每段内容简洁但可执行。`

const (
	compressionEventBlocking = "context_compression_blocking"
	compressionEventSilent   = "context_compression_silent"
)

type pendingResult struct {
	Start                  int
	End                    int
	Content                string
	SourceSliceFingerprint string
	TriggerLevel           string
	CompressedMessageCount int
}

type silentTask struct {
	done chan struct{}
}

// Coordinator 协调 silent 异步与 blocking 同步摘要压缩。
type Coordinator struct {
	client                llm.Client
	silentTriggerTokens   int
	blockingTriggerTokens int

	mu             sync.Mutex
	sessionTasks   map[string]*silentTask
	pendingResults map[string]pendingResult
}

// NewCoordinator 构造压缩协调器；silent/blocking 阈值 <=0 表示关闭对应档位。
func NewCoordinator(client llm.Client, silentTriggerTokens, blockingTriggerTokens int) *Coordinator {
	return &Coordinator{
		client:                client,
		silentTriggerTokens:   max(0, silentTriggerTokens),
		blockingTriggerTokens: max(0, blockingTriggerTokens),
		sessionTasks:          make(map[string]*silentTask),
		pendingResults:        make(map[string]pendingResult),
	}
}

// Enabled 表示是否配置了任一压缩阈值且 LLM 可用。
func (c *Coordinator) Enabled() bool {
	return c != nil && c.client != nil &&
		(c.silentTriggerTokens > 0 || c.blockingTriggerTokens > 0)
}

// MaybeHandle 在每条 message 入口处理压缩：应用 pending、触发 silent/blocking、再次尝试应用。

// 逻辑：
// 1. 回收已完成的 silent 任务；
// 2. 尝试应用 pending 压缩结果（含指纹校验）；
// 3. 按 token 阈值判定 silent / blocking（阻塞优先）；
// 4. silent 且无在跑任务时后台启动压缩；
// 5. blocking 时先等待 silent，再同步跑压缩流程；
// 6. 再次回收并尝试应用 pending。
//
// 副作用：可能修改 messages；silent 任务写入 pendingResults；SSE 类型为 context_compression_blocking / context_compression_silent。
func (c *Coordinator) MaybeHandle(
	ctx context.Context,
	sessionID, agentID string,
	hub *stream.Hub,
	messages *[]llm.Message,
) {
	if !c.Enabled() || messages == nil {
		return
	}
	c.reapFinishedTask(sessionID)
	c.tryApplyReadyResult(sessionID, agentID, hub, messages)

	decision := shouldCompress(*messages, c.silentTriggerTokens, c.blockingTriggerTokens)
	hasRunning := c.hasRunningTask(sessionID)

	if decision.Should && decision.TriggerLevel == "silent" {
		if !hasRunning {
			c.startSilentTask(sessionID, agentID, hub, snapshotMessages(*messages))
		}
	} else if decision.Should && decision.TriggerLevel == "blocking" {
		if hasRunning {
			c.waitTask(sessionID)
		}
		ok := c.runCompressionFlow(ctx, sessionID, agentID, hub, snapshotMessages(*messages), "blocking")
		if !ok {
			c.emitBlockingFailure(sessionID, agentID, hub)
		}
	}

	c.reapFinishedTask(sessionID)
	c.tryApplyReadyResult(sessionID, agentID, hub, messages)
}

// CancelSession 取消 session 在跑 silent 任务并丢弃 pending。
func (c *Coordinator) CancelSession(sessionID string) {
	c.mu.Lock()
	delete(c.pendingResults, sessionID)
	delete(c.sessionTasks, sessionID)
	c.mu.Unlock()
}

func (c *Coordinator) hasRunningTask(sessionID string) bool {
	c.mu.Lock()
	task := c.sessionTasks[sessionID]
	c.mu.Unlock()
	if task == nil {
		return false
	}
	select {
	case <-task.done:
		return false
	default:
		return true
	}
}

func (c *Coordinator) startSilentTask(sessionID, agentID string, hub *stream.Hub, messages []llm.Message) {
	task := &silentTask{done: make(chan struct{})}
	c.mu.Lock()
	c.sessionTasks[sessionID] = task
	c.mu.Unlock()

	go func() {
		defer close(task.done)
		_ = c.runCompressionFlow(context.Background(), sessionID, agentID, hub, messages, "silent")
	}()
}

func (c *Coordinator) reapFinishedTask(sessionID string) {
	c.mu.Lock()
	task := c.sessionTasks[sessionID]
	if task == nil {
		c.mu.Unlock()
		return
	}
	select {
	case <-task.done:
		delete(c.sessionTasks, sessionID)
		c.mu.Unlock()
		c.waitTask(sessionID)
	default:
		c.mu.Unlock()
	}
}

func (c *Coordinator) waitTask(sessionID string) {
	c.mu.Lock()
	task := c.sessionTasks[sessionID]
	c.mu.Unlock()
	if task == nil {
		return
	}
	<-task.done
	c.mu.Lock()
	delete(c.sessionTasks, sessionID)
	c.mu.Unlock()
}

func (c *Coordinator) runCompressionFlow(
	ctx context.Context,
	sessionID, agentID string,
	hub *stream.Hub,
	source []llm.Message,
	triggerLevel string,
) bool {
	start, end, picked, ok := selectCompressRange(source)
	if !ok {
		return false
	}
	block := buildHumanBlock(picked)
	if strings.TrimSpace(block) == "" {
		return false
	}
	c.publishCompressionEvent(sessionID, agentID, hub, triggerLevel, "start", map[string]any{
		"compression_start":        start,
		"compression_end":          end,
		"compressed_message_count": len(picked),
	})

	follow := buildHumanBlock(source[end+1:])
	prompt := fmt.Sprintf("待压缩文本块：%s；后续文本为：%s", block, follow)
	summary, err := c.client.CompleteText(ctx, llm.CompleteRequest{
		SystemPrompt: summarySystemPrompt,
		UserPrompt:   prompt,
	})
	if err != nil || strings.TrimSpace(summary) == "" {
		c.publishCompressionEvent(sessionID, agentID, hub, triggerLevel, "end", map[string]any{
			"compression_start": start,
			"compression_end":   end,
			"status":            "failed",
		})
		return false
	}
	c.mu.Lock()
	c.pendingResults[sessionID] = pendingResult{
		Start:                  start,
		End:                    end,
		Content:                strings.TrimSpace(summary),
		SourceSliceFingerprint: messagesFingerprint(source[start : end+1]),
		TriggerLevel:           triggerLevel,
		CompressedMessageCount: len(picked),
	}
	c.mu.Unlock()
	return true
}

func (c *Coordinator) tryApplyReadyResult(sessionID, agentID string, hub *stream.Hub, messages *[]llm.Message) {
	c.mu.Lock()
	pending, ok := c.pendingResults[sessionID]
	if ok {
		delete(c.pendingResults, sessionID)
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	baseEnd := map[string]any{
		"compression_start":        pending.Start,
		"compression_end":          pending.End,
		"compressed_message_count": pending.CompressedMessageCount,
	}
	if messages == nil || pending.Start < 0 || pending.End < pending.Start ||
		pending.End >= len(*messages) || strings.TrimSpace(pending.Content) == "" {
		baseEnd["status"] = "invalid"
		c.publishCompressionEvent(sessionID, agentID, hub, pending.TriggerLevel, "end", baseEnd)
		return
	}
	currentSlice := (*messages)[pending.Start : pending.End+1]
	if pending.SourceSliceFingerprint != "" &&
		pending.SourceSliceFingerprint != messagesFingerprint(currentSlice) {
		baseEnd["status"] = "stale"
		c.publishCompressionEvent(sessionID, agentID, hub, pending.TriggerLevel, "end", baseEnd)
		return
	}
	replacement := llm.Message{Role: "user", Content: pending.Content}
	rest := append([]llm.Message(nil), (*messages)[pending.End+1:]...)
	out := append(append([]llm.Message(nil), (*messages)[:pending.Start]...), replacement)
	out = append(out, rest...)
	*messages = out
	baseEnd["status"] = "applied"
	c.publishCompressionEvent(sessionID, agentID, hub, pending.TriggerLevel, "end", baseEnd)
}

func compressionEventType(triggerLevel string) string {
	if triggerLevel == "silent" {
		return compressionEventSilent
	}
	return compressionEventBlocking
}

func (c *Coordinator) publishCompressionEvent(
	sessionID, agentID string,
	hub *stream.Hub,
	triggerLevel, phase string,
	payload map[string]any,
) {
	if hub == nil {
		return
	}
	data := map[string]any{
		"phase":         phase,
		"trigger_level": triggerLevel,
		"mode":          triggerLevel,
	}
	for k, v := range payload {
		data[k] = v
	}
	hub.Publish(sessionID, agentID, compressionEventType(triggerLevel), data)
}

func (c *Coordinator) emitBlockingFailure(sessionID, agentID string, hub *stream.Hub) {
	if hub == nil {
		return
	}
	hub.Publish(sessionID, agentID, "error", map[string]any{
		"message":     "上下文阻塞压缩失败，已继续使用原始上下文。",
		"recoverable": true,
		"stage":       "summary_compression",
	})
}
