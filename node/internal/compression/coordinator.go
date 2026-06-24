package compression

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

const (
	compressionEventBlocking = "context_compression_blocking"
	compressionEventSilent   = "context_compression_silent"
)

// readyCompression 为 LLM 摘要已生成、尚未写回 session messages 的一包数据。
type readyCompression struct {
	End                    int
	Content                string
	SourceSliceFingerprint string
	TriggerLevel           string
	CompressedMessageCount int
	ApplyMode              compressApplyMode
	SidecarUsage           llm.Usage
}

// ForceResult 为手动触发阻塞压缩的结果（POST /compress）。
type ForceResult struct {
	Status                 string `json:"status"`
	TriggerLevel           string `json:"trigger_level,omitempty"`
	CompressedMessageCount int    `json:"compressed_message_count,omitempty"`
	CompressionStart       int    `json:"compression_start,omitempty"`
	CompressionEnd         int    `json:"compression_end,omitempty"`
	MessagesCount          int     `json:"messages_count,omitempty"`
	MessagesTotalTokens    int     `json:"messages_total_tokens,omitempty"`
	PromptTokens           int     `json:"prompt_tokens,omitempty"`
	CompletionTokens       int     `json:"completion_tokens,omitempty"`
	TotalTokens            int     `json:"total_tokens,omitempty"`
	TokenReductionRate     float64 `json:"token_reduction_rate,omitempty"`
	PromptCacheHitTokens   int     `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens  int     `json:"prompt_cache_miss_tokens,omitempty"`
}

type applyOutcome struct {
	status string
	count  int
	start  int
	end    int
	usage  llm.Usage
}

type compressionTask struct {
	done                   chan struct{}
	triggerLevel           string
	compressionStart       int
	compressionEnd         int
	compressedMessageCount int
}

func newCompressionTask(triggerLevel string, start, end, count int) *compressionTask {
	return &compressionTask{
		done:                   make(chan struct{}),
		triggerLevel:           triggerLevel,
		compressionStart:       start,
		compressionEnd:         end,
		compressedMessageCount: count,
	}
}

func (t *compressionTask) forceResult(status string) ForceResult {
	if t == nil {
		return ForceResult{Status: status}
	}
	return ForceResult{
		Status:                 status,
		TriggerLevel:           t.triggerLevel,
		CompressionStart:       t.compressionStart,
		CompressionEnd:         t.compressionEnd,
		CompressedMessageCount: t.compressedMessageCount,
	}
}

// Coordinator 协调 silent 异步与 blocking 同步摘要压缩。
type Coordinator struct {
	client                llm.Client
	silentTriggerTokens   int
	blockingTriggerTokens int
	rawMessageHistoryEnabled bool

	mu                sync.Mutex
	sessionTasks      map[string]*compressionTask
	readyCompressions map[string]readyCompression
	lastCompressions  map[string]LastCompressionSnapshot
	silentCooldown    map[string]silentCooldownState
	logger            *slog.Logger
}

// NewCoordinator 构造压缩协调器；silent/blocking 阈值 <=0 表示关闭对应档位。
func NewCoordinator(client llm.Client, silentTriggerTokens, blockingTriggerTokens int) *Coordinator {
	return &Coordinator{
		client:                client,
		silentTriggerTokens:   max(0, silentTriggerTokens),
		blockingTriggerTokens: max(0, blockingTriggerTokens),
		sessionTasks:          make(map[string]*compressionTask),
		readyCompressions:     make(map[string]readyCompression),
	}
}

// Enabled 表示是否配置了任一压缩阈值且 LLM 可用。
func (c *Coordinator) Enabled() bool {
	return c != nil && c.client != nil &&
		(c.silentTriggerTokens > 0 || c.blockingTriggerTokens > 0)
}

// MaybeHandle 在每条 message 入口处理压缩：应用已就绪摘要、触发 silent/blocking、再次尝试应用。

// 逻辑：
// 1. 回收已完成的 silent 任务；
// 2. 尝试写回 readyCompressions（含指纹校验）；
// 3. 按 token 阈值判定 silent / blocking（阻塞优先）；
// 4. silent 且无在跑任务、无 pending、未处于冷却期时后台启动压缩；
// 5. blocking 时先等待 silent，再同步跑压缩流程；
// 6. 再次回收并尝试写回。
//
// 副作用：可能修改 messages；silent 任务完成后写入 readyCompressions；SSE 类型为 context_compression_blocking / context_compression_silent。
func (c *Coordinator) MaybeHandle(
	ctx context.Context,
	sessionID, agentID string,
	hub *stream.Hub,
	messages *[]llm.Message,
	prefix SidecarPrefix,
) {
	if !c.Enabled() || messages == nil {
		return
	}
	c.reapFinishedTask(sessionID)
	c.applyReadyCompression(sessionID, agentID, hub, messages)

	decision := evaluateCompression(*messages, c.silentTriggerTokens, c.blockingTriggerTokens)
	hasRunning := c.hasRunningTask(sessionID)

	if decision.Decision.Should && decision.Decision.TriggerLevel == "silent" {
		if !hasRunning && c.shouldStartSilent(sessionID, *messages) {
			c.startSilentTask(sessionID, agentID, hub, prefix, decision.Plan, snapshotMessages(*messages))
		}
	} else if decision.Decision.Should && decision.Decision.TriggerLevel == "blocking" {
		if hasRunning {
			c.waitTask(sessionID)
		}
		ok := c.runCompressionFlow(ctx, sessionID, agentID, hub, SidecarInput{
			SidecarPrefix: prefix,
			Messages:      snapshotMessages(*messages),
		}, decision.Plan, "blocking")
		if !ok {
			c.emitBlockingFailure(sessionID, agentID, hub)
		}
	}

	c.reapFinishedTask(sessionID)
	c.applyReadyCompression(sessionID, agentID, hub, messages)
}

// ForceBlocking 手动执行一次阻塞压缩：忽略 token 阈值，同步 LLM 摘要并立即应用。
//
// 返回 status：applied / failed / noop / stale / invalid / disabled / in_progress。
func (c *Coordinator) ForceBlocking(
	ctx context.Context,
	sessionID, agentID string,
	hub *stream.Hub,
	messages *[]llm.Message,
	prefix SidecarPrefix,
) ForceResult {
	if c == nil || c.client == nil {
		return ForceResult{Status: "disabled"}
	}
	if messages == nil {
		return ForceResult{Status: "noop"}
	}
	plan, ok := buildCompressionPlan(*messages)
	if !ok {
		return ForceResult{Status: "noop"}
	}
	picked := compressionSlice(*messages, plan)
	compressStart := leadingSystemSkip(*messages)
	if running := c.runningTask(sessionID); running != nil {
		return running.forceResult("in_progress")
	}

	task := newCompressionTask("blocking", compressStart, plan.End, len(picked))
	if !c.registerTask(sessionID, task) {
		if running := c.runningTask(sessionID); running != nil {
			return running.forceResult("in_progress")
		}
		return ForceResult{Status: "failed"}
	}
	defer c.unregisterTask(sessionID, task)

	if !c.runCompressionFlow(ctx, sessionID, agentID, hub, SidecarInput{
		SidecarPrefix: prefix,
		Messages:      snapshotMessages(*messages),
	}, plan, "blocking") {
		return ForceResult{Status: "failed"}
	}
	out := c.applyReadyCompression(sessionID, agentID, hub, messages)
	if out.status == "" {
		return ForceResult{Status: "failed"}
	}
	out.usage.Normalize()
	return ForceResult{
		Status:                 out.status,
		TriggerLevel:           "blocking",
		CompressedMessageCount: out.count,
		CompressionStart:       out.start,
		CompressionEnd:         out.end,
		MessagesCount:          len(*messages),
		MessagesTotalTokens:    llm.EstimateMessageTokens(*messages),
		PromptTokens:           out.usage.PromptTokens,
		CompletionTokens:       out.usage.CompletionTokens,
		TotalTokens:            out.usage.TotalTokens,
		TokenReductionRate:     tokenReductionRate(out.usage.PromptTokens, out.usage.CompletionTokens),
		PromptCacheHitTokens:   out.usage.PromptCachedTokens(),
		PromptCacheMissTokens:  out.usage.PromptCacheMissTokensEffective(),
	}
}

// CancelSession 取消 session 在跑 silent 任务并丢弃已就绪未写回的压缩摘要。
func (c *Coordinator) CancelSession(sessionID string) {
	c.mu.Lock()
	delete(c.readyCompressions, sessionID)
	delete(c.sessionTasks, sessionID)
	delete(c.silentCooldown, sessionID)
	c.mu.Unlock()
}

func (c *Coordinator) runningTask(sessionID string) *compressionTask {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runningTaskLocked(sessionID)
}

func (c *Coordinator) runningTaskLocked(sessionID string) *compressionTask {
	task := c.sessionTasks[sessionID]
	if task == nil {
		return nil
	}
	select {
	case <-task.done:
		return nil
	default:
		return task
	}
}

func (c *Coordinator) registerTask(sessionID string, task *compressionTask) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runningTaskLocked(sessionID) != nil {
		return false
	}
	c.sessionTasks[sessionID] = task
	return true
}

func (c *Coordinator) unregisterTask(sessionID string, task *compressionTask) {
	c.mu.Lock()
	if c.sessionTasks[sessionID] == task {
		delete(c.sessionTasks, sessionID)
	}
	c.mu.Unlock()
	close(task.done)
}

func (c *Coordinator) hasRunningTask(sessionID string) bool {
	return c.runningTask(sessionID) != nil
}

func (c *Coordinator) startSilentTask(
	sessionID, agentID string,
	hub *stream.Hub,
	prefix SidecarPrefix,
	plan compressionPlan,
	messages []llm.Message,
) {
	picked := compressionSlice(messages, plan)
	if !hasCompressibleContent(picked) {
		return
	}
	task := newCompressionTask("silent", leadingSystemSkip(messages), plan.End, len(picked))
	if !c.registerTask(sessionID, task) {
		return
	}

	frozen := SidecarInput{
		SidecarPrefix: prefix,
		Messages:      snapshotMessages(messages),
	}
	go func(input SidecarInput, frozenPlan compressionPlan) {
		defer c.unregisterTask(sessionID, task)
		_ = c.runCompressionFlow(context.Background(), sessionID, agentID, hub, input, frozenPlan, "silent")
	}(frozen, plan)
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
		return
	default:
		c.mu.Unlock()
	}
}

func (c *Coordinator) waitTask(sessionID string) {
	task := c.runningTask(sessionID)
	if task == nil {
		return
	}
	<-task.done
	c.mu.Lock()
	if c.sessionTasks[sessionID] == task {
		delete(c.sessionTasks, sessionID)
	}
	c.mu.Unlock()
}

func (c *Coordinator) runCompressionFlow(
	ctx context.Context,
	sessionID, agentID string,
	hub *stream.Hub,
	input SidecarInput,
	plan compressionPlan,
	triggerLevel string,
) bool {
	picked := compressionSlice(input.Messages, plan)
	if !hasCompressibleContent(picked) {
		return false
	}
	compressStart := leadingSystemSkip(input.Messages)
	input.End = plan.End
	input.SidecarAppend = plan.SidecarAppend
	c.publishCompressionEvent(sessionID, agentID, hub, triggerLevel, "start", map[string]any{
		"compression_start":        compressStart,
		"compression_end":          plan.End,
		"compressed_message_count": len(picked),
		"apply_mode":               string(plan.ApplyMode),
		"sidecar_append":           string(plan.SidecarAppend),
	})

	chatReq := BuildSidecarChatRequest(input, summaryUserPrompt)
	summary, sidecarUsage, err := Summarize(ctx, c.client, chatReq)
	if err != nil || strings.TrimSpace(summary) == "" {
		c.publishCompressionEvent(sessionID, agentID, hub, triggerLevel, "end", map[string]any{
			"compression_start": compressStart,
			"compression_end":   plan.End,
			"status":            "failed",
		})
		return false
	}
	summary = FinalizeCompressionSummary(summary, sessionID, c.rawMessageHistoryEnabled, time.Now())
	c.mu.Lock()
	c.readyCompressions[sessionID] = readyCompression{
		End:                    plan.End,
		Content:                strings.TrimSpace(summary),
		SourceSliceFingerprint: compressionSourceFingerprint(input.Messages, plan),
		TriggerLevel:           triggerLevel,
		CompressedMessageCount: len(picked),
		ApplyMode:              plan.ApplyMode,
		SidecarUsage:           sidecarUsage,
	}
	c.mu.Unlock()
	return true
}

func (c *Coordinator) applyReadyCompression(sessionID, agentID string, hub *stream.Hub, messages *[]llm.Message) applyOutcome {
	c.mu.Lock()
	ready, ok := c.readyCompressions[sessionID]
	if ok {
		delete(c.readyCompressions, sessionID)
	}
	c.mu.Unlock()
	if !ok {
		return applyOutcome{}
	}
	compressStart := 0
	if messages != nil {
		compressStart = leadingSystemSkip(*messages)
	}
	baseEnd := map[string]any{
		"compression_start":        compressStart,
		"compression_end":          ready.End,
		"compressed_message_count": ready.CompressedMessageCount,
	}
	out := applyOutcome{
		status: "invalid",
		count:  ready.CompressedMessageCount,
		start:  compressStart,
		end:    ready.End,
	}
	if messages == nil || ready.End < compressStart ||
		ready.End >= len(*messages) || strings.TrimSpace(ready.Content) == "" {
		baseEnd["status"] = out.status
		c.publishCompressionEvent(sessionID, agentID, hub, ready.TriggerLevel, "end", baseEnd)
		return out
	}
	plan := compressionPlan{
		End:       ready.End,
		ApplyMode: ready.ApplyMode,
	}
	merged, status := applyCompressionReplacement(*messages, plan, ready.Content, ready.SourceSliceFingerprint)
	if status == "" {
		status = "invalid"
	}
	out.status = status
	baseEnd["status"] = out.status
	baseEnd["apply_mode"] = string(plan.ApplyMode)
	if status != "applied" {
		c.publishCompressionEvent(sessionID, agentID, hub, ready.TriggerLevel, "end", baseEnd)
		return out
	}
	attachCompressionUsageMetrics(baseEnd, ready.SidecarUsage)
	out.usage = ready.SidecarUsage
	*messages = merged
	c.markSilentCooldownApplied(sessionID, merged)
	c.recordLastCompression(sessionID, buildLastCompressionSnapshot(ready, ready.SidecarUsage))
	c.publishCompressionEvent(sessionID, agentID, hub, ready.TriggerLevel, "end", baseEnd)
	return out
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
