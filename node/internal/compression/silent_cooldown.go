package compression

import (
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// SilentCooldownDuration 为 apply 后抑制重复 silent 侧车的最短间隔。
// 与 SilentCooldownTokenGrowth 为「或」关系：满足其一即可再次触发 silent。
var SilentCooldownDuration = 60 * time.Second

// SilentCooldownTokenGrowth 为自上次成功 apply 起 messages 估算 token 增量门槛。
var SilentCooldownTokenGrowth = 4000

type silentCooldownState struct {
	lastAppliedAt     time.Time
	lastAppliedTokens int
}

func (c *Coordinator) shouldStartSilent(sessionID string, messages []llm.Message) bool {
	if c == nil || sessionID == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.runningTaskLocked(sessionID) != nil {
		return false
	}
	if _, pending := c.readyCompressions[sessionID]; pending {
		return false
	}

	state, tracked := c.silentCooldown[sessionID]
	if !tracked || state.lastAppliedAt.IsZero() {
		return true
	}

	if time.Since(state.lastAppliedAt) >= SilentCooldownDuration {
		return true
	}
	current := llm.EstimateMessageTokens(messages)
	if current-state.lastAppliedTokens >= SilentCooldownTokenGrowth {
		return true
	}
	return false
}

func (c *Coordinator) markSilentCooldownApplied(sessionID string, messages []llm.Message) {
	if c == nil || sessionID == "" || messages == nil {
		return
	}
	c.mu.Lock()
	if c.silentCooldown == nil {
		c.silentCooldown = make(map[string]silentCooldownState)
	}
	c.silentCooldown[sessionID] = silentCooldownState{
		lastAppliedAt:     time.Now(),
		lastAppliedTokens: llm.EstimateMessageTokens(messages),
	}
	logger := c.logger
	c.mu.Unlock()

	if logger != nil {
		logger.Debug("silent compression cooldown armed",
			"session_id", sessionID,
			"messages_tokens", llm.EstimateMessageTokens(messages),
			"cooldown_duration", SilentCooldownDuration.String(),
			"cooldown_token_growth", SilentCooldownTokenGrowth,
		)
	}
}
