package llm

import "encoding/json"

// PromptTokensDetails 为 OpenAI usage.prompt_tokens_details（兼容 prompt_token_details）。
type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
	AudioTokens  int `json:"audio_tokens"`
}

// CompletionTokensDetails 为 OpenAI usage.completion_tokens_details。
type CompletionTokensDetails struct {
	ReasoningTokens          int `json:"reasoning_tokens"`
	AudioTokens              int `json:"audio_tokens"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
}

// Usage 表示一次 completion 的 token 用量（OpenAI + DeepSeek 兼容）。
//
// OpenAI：prompt_tokens_details.cached_tokens
// DeepSeek：prompt_cache_hit_tokens / prompt_cache_miss_tokens（见 Normalize）
type Usage struct {
	PromptTokens             int                      `json:"prompt_tokens"`
	CompletionTokens         int                      `json:"completion_tokens"`
	TotalTokens              int                      `json:"total_tokens"`
	PromptCacheHitTokens     int                      `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens    int                      `json:"prompt_cache_miss_tokens"`
	PromptTokensDetails      *PromptTokensDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails  *CompletionTokensDetails `json:"completion_tokens_details"`
}

// UnmarshalJSON 解析 usage；兼容 prompt_token_details / completion_token_details 别名，
// 以及部分网关把 reasoning_tokens 放在 usage 顶层的情况。
func (u *Usage) UnmarshalJSON(data []byte) error {
	type usageFields Usage
	aux := struct {
		usageFields
		PromptTokenDetails     *PromptTokensDetails     `json:"prompt_token_details"`
		CompletionTokenDetails *CompletionTokensDetails `json:"completion_token_details"`
		ReasoningTokens        int                      `json:"reasoning_tokens"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*u = Usage(aux.usageFields)
	if u.PromptTokensDetails == nil && aux.PromptTokenDetails != nil {
		u.PromptTokensDetails = aux.PromptTokenDetails
	}
	if u.CompletionTokensDetails == nil && aux.CompletionTokenDetails != nil {
		u.CompletionTokensDetails = aux.CompletionTokenDetails
	}
	if aux.ReasoningTokens > 0 && u.CompletionReasoningTokens() <= 0 {
		u.ensureCompletionDetails().ReasoningTokens = aux.ReasoningTokens
	}
	u.Normalize()
	return nil
}

// Normalize 将 OpenAI / DeepSeek 不同字段名对齐为统一语义。
func (u *Usage) Normalize() {
	u.clampNonNegative()

	cached := u.PromptCachedTokens()
	hit := max(0, u.PromptCacheHitTokens)
	miss := max(0, u.PromptCacheMissTokens)

	// DeepSeek → OpenAI 形态：hit 写入 cached_tokens
	if cached <= 0 && hit > 0 {
		u.ensurePromptDetails().CachedTokens = hit
		cached = hit
	}
	// OpenAI → DeepSeek 形态：cached_tokens 写入 hit
	if hit <= 0 && cached > 0 {
		u.PromptCacheHitTokens = cached
		hit = cached
	}

	prompt := max(0, u.PromptTokens)
	if prompt > 0 {
		if miss <= 0 && hit > 0 && hit <= prompt {
			miss = prompt - hit
		}
		if hit <= 0 && miss > 0 && miss <= prompt {
			hit = prompt - miss
		}
		if hit > prompt {
			hit = prompt
		}
		if miss > prompt {
			miss = prompt
		}
		if hit > 0 && miss <= 0 && prompt > hit {
			miss = prompt - hit
		}
	}

	u.PromptCacheHitTokens = hit
	u.PromptCacheMissTokens = miss
	if hit > 0 || cached > 0 {
		u.ensurePromptDetails().CachedTokens = max(hit, cached)
	}

	if u.TotalTokens <= 0 && (prompt > 0 || u.CompletionTokens > 0) {
		u.TotalTokens = prompt + max(0, u.CompletionTokens)
	}
	if prompt <= 0 && hit+miss > 0 {
		u.PromptTokens = hit + miss
	}
}

func (u *Usage) clampNonNegative() {
	u.PromptTokens = max(0, u.PromptTokens)
	u.CompletionTokens = max(0, u.CompletionTokens)
	u.TotalTokens = max(0, u.TotalTokens)
	u.PromptCacheHitTokens = max(0, u.PromptCacheHitTokens)
	u.PromptCacheMissTokens = max(0, u.PromptCacheMissTokens)
}

func (u *Usage) ensurePromptDetails() *PromptTokensDetails {
	if u.PromptTokensDetails == nil {
		u.PromptTokensDetails = &PromptTokensDetails{}
	}
	return u.PromptTokensDetails
}

func (u *Usage) ensureCompletionDetails() *CompletionTokensDetails {
	if u.CompletionTokensDetails == nil {
		u.CompletionTokensDetails = &CompletionTokensDetails{}
	}
	return u.CompletionTokensDetails
}

// AccumulateFrom 将另一段 completion 的 usage 累加到当前快照（工具循环多轮 LLM 调用）。
func (u *Usage) AccumulateFrom(other Usage) {
	if u == nil {
		return
	}
	other.Normalize()
	u.Normalize()
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
	u.TotalTokens += other.TotalTokens
	u.PromptCacheHitTokens += other.PromptCacheHitTokens
	u.PromptCacheMissTokens += other.PromptCacheMissTokens
	if other.PromptTokensDetails != nil {
		d := u.ensurePromptDetails()
		d.CachedTokens += other.PromptTokensDetails.CachedTokens
		d.AudioTokens += other.PromptTokensDetails.AudioTokens
	}
	if rt := other.CompletionReasoningTokens(); rt > 0 {
		u.ensureCompletionDetails().ReasoningTokens += rt
	}
	u.Normalize()
}

// PromptCachedTokens 返回 prompt 侧 cache hit token 数（OpenAI cached_tokens 或 DeepSeek hit 对齐后）。
func (u Usage) PromptCachedTokens() int {
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens
	}
	return max(0, u.PromptCacheHitTokens)
}

// PromptCacheMissTokensEffective 返回 cache miss token 数；缺失时由 prompt - hit 推导。
func (u Usage) PromptCacheMissTokensEffective() int {
	if u.PromptCacheMissTokens > 0 {
		return u.PromptCacheMissTokens
	}
	prompt := max(0, u.PromptTokens)
	hit := u.PromptCachedTokens()
	if prompt > hit {
		return prompt - hit
	}
	return 0
}

// PromptCacheHitRate 返回 cache 命中率 [0,1]；prompt 为 0 时返回 -1 表示不可用。
func (u Usage) PromptCacheHitRate() float64 {
	prompt := max(0, u.PromptTokens)
	if prompt <= 0 {
		return -1
	}
	hit := u.PromptCachedTokens()
	if hit <= 0 {
		return 0
	}
	if hit > prompt {
		hit = prompt
	}
	return float64(hit) / float64(prompt)
}

// PromptAudioTokens 返回 prompt_tokens_details.audio_tokens（缺失时为 0）。
func (u Usage) PromptAudioTokens() int {
	if u.PromptTokensDetails == nil {
		return 0
	}
	return max(0, u.PromptTokensDetails.AudioTokens)
}

// CompletionReasoningTokens 返回 completion_tokens_details.reasoning_tokens。
func (u Usage) CompletionReasoningTokens() int {
	if u.CompletionTokensDetails == nil {
		return 0
	}
	return max(0, u.CompletionTokensDetails.ReasoningTokens)
}

// SSEPayload 转为 SSE usage 事件扁平字段（Client / Textual TUI 共用）。
func (u Usage) SSEPayload() map[string]any {
	norm := u
	norm.Normalize()
	payload := map[string]any{
		"prompt_tokens":            max(0, norm.PromptTokens),
		"completion_tokens":        max(0, norm.CompletionTokens),
		"total_tokens":             max(0, norm.TotalTokens),
		"prompt_cached_tokens":     norm.PromptCachedTokens(),
		"prompt_cache_hit_tokens":  norm.PromptCachedTokens(),
		"prompt_cache_miss_tokens": norm.PromptCacheMissTokensEffective(),
		"prompt_audio_tokens":      norm.PromptAudioTokens(),
		"reasoning_tokens":         norm.CompletionReasoningTokens(),
	}
	if rate := norm.PromptCacheHitRate(); rate >= 0 {
		payload["prompt_cache_hit_rate"] = rate
	}
	if norm.PromptTokensDetails != nil {
		payload["prompt_tokens_details"] = norm.PromptTokensDetails
	}
	if norm.CompletionTokensDetails != nil {
		payload["completion_tokens_details"] = norm.CompletionTokensDetails
	}
	return payload
}
