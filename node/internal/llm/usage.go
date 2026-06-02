package llm

import "encoding/json"

// PromptTokensDetails 为 OpenAI usage.prompt_tokens_details（兼容 prompt_token_details）。
type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
	AudioTokens  int `json:"audio_tokens"`
}

// Usage 表示一次 completion 的 token 用量（含可选 prompt cache 明细）。
type Usage struct {
	PromptTokens          int                  `json:"prompt_tokens"`
	CompletionTokens      int                  `json:"completion_tokens"`
	TotalTokens           int                  `json:"total_tokens"`
	PromptCacheHitTokens  int                  `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int                  `json:"prompt_cache_miss_tokens"`
	PromptTokensDetails   *PromptTokensDetails `json:"prompt_tokens_details"`
}

// UnmarshalJSON 解析 usage；兼容 prompt_token_details 别名。
func (u *Usage) UnmarshalJSON(data []byte) error {
	type usageFields Usage
	aux := struct {
		usageFields
		PromptTokenDetails *PromptTokensDetails `json:"prompt_token_details"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*u = Usage(aux.usageFields)
	if u.PromptTokensDetails == nil && aux.PromptTokenDetails != nil {
		u.PromptTokensDetails = aux.PromptTokenDetails
	}
	return nil
}

// PromptCachedTokens 返回 prompt_tokens_details.cached_tokens（缺失时为 0）。
func (u Usage) PromptCachedTokens() int {
	if u.PromptTokensDetails == nil {
		return 0
	}
	return max(0, u.PromptTokensDetails.CachedTokens)
}

// PromptAudioTokens 返回 prompt_tokens_details.audio_tokens（缺失时为 0）。
func (u Usage) PromptAudioTokens() int {
	if u.PromptTokensDetails == nil {
		return 0
	}
	return max(0, u.PromptTokensDetails.AudioTokens)
}

// SSEPayload 转为 SSE usage 事件扁平字段（与 Python agent_service 映射对齐）。
func (u Usage) SSEPayload() map[string]any {
	return map[string]any{
		"prompt_tokens":            max(0, u.PromptTokens),
		"completion_tokens":        max(0, u.CompletionTokens),
		"total_tokens":             max(0, u.TotalTokens),
		"prompt_cached_tokens":     u.PromptCachedTokens(),
		"prompt_cache_hit_tokens":  max(0, u.PromptCacheHitTokens),
		"prompt_cache_miss_tokens": max(0, u.PromptCacheMissTokens),
		"prompt_audio_tokens":      u.PromptAudioTokens(),
	}
}
