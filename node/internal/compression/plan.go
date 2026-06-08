package compression

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type compressDecision struct {
	Should       bool
	TriggerLevel string // none / silent / blocking
	TotalTokens  int
}

func shouldCompress(messages []llm.Message, silentThreshold, blockingThreshold int) compressDecision {
	silentThreshold = max(0, silentThreshold)
	blockingThreshold = max(0, blockingThreshold)
	total := llm.EstimateMessageTokens(messages)

	var level string
	if blockingThreshold > 0 && total >= blockingThreshold {
		level = "blocking"
	} else if silentThreshold > 0 && total >= silentThreshold {
		level = "silent"
	} else {
		return compressDecision{Should: false, TriggerLevel: "none", TotalTokens: total}
	}

	if _, _, _, ok := selectCompressRange(messages); !ok {
		return compressDecision{Should: false, TriggerLevel: level, TotalTokens: total}
	}
	return compressDecision{Should: true, TriggerLevel: level, TotalTokens: total}
}

func selectCompressRange(messages []llm.Message) (start, end int, picked []llm.Message, ok bool) {
	lastAssistant := -1
	for i, m := range messages {
		if m.Role == "assistant" {
			lastAssistant = i
		}
	}
	if lastAssistant <= 0 {
		return 0, 0, nil, false
	}
	candidate := append([]llm.Message(nil), messages[:lastAssistant]...)
	if len(candidate) == 0 {
		return 0, 0, nil, false
	}
	return 0, lastAssistant - 1, candidate, true
}

func buildHumanBlock(messages []llm.Message) string {
	if len(messages) == 0 {
		return "（无后续文本）"
	}
	var b strings.Builder
	for i, m := range messages {
		if m.Role == "system" {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" && len(m.ToolCalls) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("[%d] role=%s content=%s\n", i+1, m.Role, truncate(content, 800)))
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return "（无后续文本）"
	}
	return text
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
