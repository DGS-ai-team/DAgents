package memory

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

// The budgets below are model-visible budgets. Storage is intentionally not
// bounded by them; only a rendered context or a tool response is bounded.
const (
	DefaultMemoryContextTokenBudget = 2000
	DefaultMemoryCoreTokenBudget    = 800
	DefaultMemoryRecallTokenBudget  = 1200

	DefaultMemorySearchLimit       = 6
	DefaultMemorySearchTokenBudget = 1200
	DefaultMemorySearchEntryBudget = 160
	DefaultMemoryGetTokenBudget    = 2000
	DefaultMemoryGetContentBudget  = 1500
	DefaultMemoryQueryTokenBudget  = 160
	DefaultMemoryMaxSearchTerms    = 24
)

// BoundedText is the observable result of clipping text for a model-facing
// response. OriginalTokens lets the model distinguish a complete value from
// a preview without receiving the original payload.
type BoundedText struct {
	Text           string `json:"text"`
	OriginalTokens int    `json:"original_tokens"`
	ReturnedTokens int    `json:"returned_tokens"`
	Truncated      bool   `json:"truncated"`
}

func BoundText(text string, maxTokens int) BoundedText {
	if maxTokens < 0 {
		maxTokens = 0
	}
	original := tokens.EstimateInt(text)
	clipped, truncated := tokens.ClipToTokenBudget(text, float64(maxTokens))
	return BoundedText{
		Text:           clipped,
		OriginalTokens: original,
		ReturnedTokens: tokens.EstimateInt(clipped),
		Truncated:      truncated || strings.TrimSpace(clipped) != strings.TrimSpace(text),
	}
}
