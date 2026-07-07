package turn

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// BuildHITLRequiredPayload 构造 hitl_required SSE 同构的 message 与 items。
func BuildHITLRequiredPayload(items []PendingHITLItem) (message string, sseItems []map[string]any) {
	return buildHITLRequiredPayload(items)
}

// BuildHITLRequiredSnapshot 将 PendingHITL 转为 hydrate / hitl_required 同构快照；无 pending 时返回 nil。
func BuildHITLRequiredSnapshot(pending *PendingHITL) map[string]any {
	if pending == nil || len(pending.Items) == 0 {
		return nil
	}
	pending.Normalize()
	message, items := buildHITLRequiredPayload(pending.Items)
	return map[string]any{
		"hitl_id":      StableHITLID(pending),
		"message":      message,
		"items":        items,
		"display_type": "normal_text",
	}
}

// StableHITLID 基于 pending tool call id 生成确定性 hitl_id（SSE 运行时 id 未持久化）。
func StableHITLID(pending *PendingHITL) string {
	pending.Normalize()
	ids := make([]string, 0, len(pending.Items))
	for _, item := range pending.Items {
		if id := strings.TrimSpace(item.ToolCall.ID); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return "hitl-" + hex.EncodeToString(sum[:8])
}
