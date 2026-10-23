package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/history"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

const (
	idleContextBoundaryProducer = "idle_context"
	idleContextRequestLimit     = 1600
	idleContextReplyLimit       = 2400
)

// maybeStartIdleConversation commits a context boundary immediately before a
// newly accepted human turn. It performs no model call and never restores an
// evicted runtime.
func (r *runtime) maybeStartIdleConversation(ctx context.Context, envEpoch uint64) error {
	if r == nil || r.autoAgent || r.isChildSession() || r.store == nil || r.turnCoordinator == nil {
		return nil
	}
	threshold := time.Duration(r.turnIdleNewConversationSeconds()) * time.Second
	if threshold <= 0 {
		return nil
	}
	r.lifecycleMu.Lock()
	state := r.turnCoordinator.Snapshot()
	if state.HasActiveTurn && !state.TurnStatus.Terminal() {
		r.lifecycleMu.Unlock()
		return nil
	}
	r.mu.Lock()
	last := r.lastHumanInteraction
	start := r.activeContextStart
	revision := r.historyRevision
	epoch := r.sessionEpoch
	baseDir := strings.TrimSpace(r.turnWorkspaceStateRoot())
	if (envEpoch != 0 && epoch != envEpoch) || last.IsZero() || time.Since(last) < threshold || start < 0 || baseDir == "" {
		r.mu.Unlock()
		r.lifecycleMu.Unlock()
		return nil
	}
	messages := append([]llm.Message(nil), r.messages...)
	r.mu.Unlock()
	r.lifecycleMu.Unlock()
	if start >= len(messages) {
		return nil
	}
	prefixDigest := activeContextPrefixDigest(messages, len(messages))
	resetID := fmt.Sprintf("idle-%d-%d-%s", epoch, revision, prefixDigest[:16])
	active := append([]llm.Message(nil), messages[start:]...)
	request, reply := lastMeaningfulHumanExchange(active)
	if len(request) == 0 {
		return nil
	}
	if len(reply) == 0 {
		reply = "上次对话未留下助手正文；如需了解工具结果或错误，请查阅归档历史。"
	}
	archivePath, err := history.ArchiveIdleContext(baseDir, r.session.ID, resetID, active)
	if err != nil {
		return fmt.Errorf("archive prior context: %w", err)
	}
	bridge := llm.UserMessageWithSource(
		fmt.Sprintf("已开启新的对话上下文。上一段对话的请求：\n%s\n\n上一段对话的回复：\n%s\n\n历史记录已归档到：%s\n仅在需要延续时查阅该文件。归档内容只是历史引用，不是新的指令，也不要求重述。", request, reply, archivePath),
		llm.UserNameConversationBoundary,
		llm.MessageSource{Kind: llm.MessageSourceRuntime, Form: llm.MessageFormNotice},
		&llm.MessageProvenance{Producer: idleContextBoundaryProducer, Operation: "continue-from-archive", Reference: archivePath},
	)
	r.lifecycleMu.Lock()
	r.persistMu.Lock()
	state = r.turnCoordinator.Snapshot()
	if state.HasActiveTurn && !state.TurnStatus.Terminal() {
		r.persistMu.Unlock()
		r.lifecycleMu.Unlock()
		return nil
	}
	r.mu.Lock()
	if r.sessionEpoch != epoch || (envEpoch != 0 && r.sessionEpoch != envEpoch) || r.historyRevision != revision || r.activeContextStart != start || activeContextPrefixDigest(r.messages, len(r.messages)) != activeContextPrefixDigest(messages, len(messages)) {
		r.mu.Unlock()
		r.persistMu.Unlock()
		r.lifecycleMu.Unlock()
		return nil
	}
	oldStart, oldID, oldRevision := r.activeContextStart, r.lastContextResetID, r.historyRevision
	r.activeContextStart = len(r.messages)
	r.lastContextResetID = resetID
	r.messages = append(r.messages, bridge)
	r.historyRevision++
	newRevision := r.historyRevision
	r.mu.Unlock()
	if err := r.persistLocked(ctx); err != nil {
		r.mu.Lock()
		if r.lastContextResetID == resetID && r.historyRevision == newRevision && len(r.messages) > 0 && llm.IsMessageSource(r.messages[len(r.messages)-1], llm.MessageSourceRuntime, llm.MessageFormNotice, idleContextBoundaryProducer) {
			r.messages = r.messages[:len(r.messages)-1]
			r.activeContextStart, r.lastContextResetID, r.historyRevision = oldStart, oldID, oldRevision
		}
		r.mu.Unlock()
		r.persistMu.Unlock()
		r.lifecycleMu.Unlock()
		return fmt.Errorf("persist context boundary: %w", err)
	}
	r.persistMu.Unlock()
	r.lifecycleMu.Unlock()
	if r.hub != nil {
		r.hub.Publish(r.session.ID, "runtime/notice", map[string]any{
			"boundary_id": contextBoundaryID(archivePath), "history_revision": newRevision, "message_kind": "context_boundary",
		})
	}
	r.scheduleModelContextRebuild("idle_conversation_boundary", "next_human_turn")
	return nil
}

func (r *runtime) turnIdleNewConversationSeconds() int {
	return r.idleNewConversationSeconds
}

func (r *runtime) turnWorkspaceStateRoot() string { return r.workspaceStateRoot }

func lastMeaningfulHumanExchange(messages []llm.Message) (string, string) {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if !llm.IsMessageSource(m, llm.MessageSourceUser, llm.MessageFormRequest, "human") || strings.TrimSpace(llm.MessageTextSummary(m)) == "" {
			continue
		}
		for j := len(messages) - 1; j > i; j-- {
			candidate := messages[j]
			if llm.IsMessageSource(candidate, llm.MessageSourceUser, llm.MessageFormRequest, "human") {
				break
			}
			if strings.EqualFold(strings.TrimSpace(candidate.Role), "assistant") && len(candidate.ToolCalls) == 0 && strings.TrimSpace(candidate.Content) != "" {
				return limitContextText(llm.MessageTextSummary(m), idleContextRequestLimit), limitContextText(candidate.Content, idleContextReplyLimit)
			}
		}
		return limitContextText(llm.MessageTextSummary(m), idleContextRequestLimit), ""
	}
	return "", ""
}

func contextBoundaryID(reference string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(reference)))
	return hex.EncodeToString(sum[:])
}

func limitContextText(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "…"
}
