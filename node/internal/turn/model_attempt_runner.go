package turn

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// runModelRequest owns provider attempts inside one logical Step. Keeping the
// retry loop here makes the Step boundary explicit: a transient provider
// failure creates another ModelAttempt, while the caller still owns assistant
// history, ToolBatch creation, HITL, and continuation decisions.
func (o *Orchestrator) runModelRequest(
	ctx context.Context,
	sessionID string,
	systemPrompt string,
	messages []llm.Message,
	toolDefs []tools.ToolDef,
	requestDigest string,
	stepIndex int,
) (llm.ChatResult, error) {
	var result llm.ChatResult
	var err error
	var lifecycleErrMu sync.Mutex
	var lifecycleErr error
	recordLifecycleErr := func(err error) {
		if err == nil {
			return
		}
		lifecycleErrMu.Lock()
		if lifecycleErr == nil {
			lifecycleErr = err
		}
		lifecycleErrMu.Unlock()
	}
	getLifecycleErr := func() error {
		lifecycleErrMu.Lock()
		defer lifecycleErrMu.Unlock()
		return lifecycleErr
	}
	publishedToolPartial := make(map[int]string)
	var nextRetryDelay time.Duration
	for attempt := 0; ; attempt++ {
		if !o.executionBoundaryOpen(ctx) {
			return result, context.Canceled
		}
		if attempt > 0 {
			if o.modelRetryCheck != nil {
				allowed, reason := o.modelRetryCheck(sessionID)
				if !allowed {
					if strings.TrimSpace(reason) == "" {
						reason = "turn_budget"
					}
					return result, fmt.Errorf("%w: %s", ErrBudgetExhausted, reason)
				}
			}
			if retryErr := waitModelRetry(ctx, attempt, nextRetryDelay); retryErr != nil {
				return result, retryErr
			}
		}
		if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
			Type: CommandModelRequestStarted, At: time.Now().UTC(),
			RequestDigest: requestDigest, MaxAttempts: o.modelRetryLimit + 1, Reason: "model_request_started",
		}); err != nil {
			if !o.executionBoundaryOpen(ctx) {
				return result, context.Canceled
			}
			return result, fmt.Errorf("record model request start: %w", err)
		}
		o.resetUsageAttempt(sessionID, stepIndex)
		attemptProducedOutput := false
		var streamedContent strings.Builder
		result, err = o.llm.StreamChat(ctx, llm.ChatRequest{
			SystemPrompt: systemPrompt,
			Messages:     messages,
			Tools:        toolDefs,
		}, llm.StreamHandler{
			OnDelta: func(delta string) {
				if delta != "" {
					attemptProducedOutput = true
					streamedContent.WriteString(delta)
				}
				o.publishAssistant(sessionID, delta)
			},
			OnReasoningDelta: func(delta string) {
				if delta != "" {
					attemptProducedOutput = true
				}
				o.publishReasoning(sessionID, delta)
			},
			OnToolCallDelta: func(calls []llm.ToolCall) {
				for i, tc := range calls {
					if tc.ID != "" || tc.Type != "" || tc.Function.Name != "" || tc.Function.Arguments != "" {
						attemptProducedOutput = true
					}
					fingerprint := tc.ID + "\x1e" + tc.Function.Name + "\x1e" + tc.Function.Arguments
					if publishedToolPartial[i] == fingerprint {
						continue
					}
					publishedToolPartial[i] = fingerprint
					o.publishToolCall(sessionID, tc, true, i)
				}
			},
			OnUsage: func(usage llm.Usage) {
				usage.Normalize()
				o.publishUsage(sessionID, stepIndex, usage)
				recordLifecycleErr(o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
					Type: CommandModelUsageRecorded, At: time.Now().UTC(),
					Usage: StepUsage{
						InputTokens:                usage.PromptTokens,
						OutputTokens:               usage.CompletionTokens,
						TotalTokens:                usage.TotalTokens,
						PromptCacheHitTokens:       usage.PromptCachedTokens(),
						PromptCacheMissTokens:      usage.PromptCacheMissTokensEffective(),
						PromptCacheMetricsObserved: usage.HasPromptCacheMetrics(),
						ReasoningTokens:            usage.CompletionReasoningTokens(),
					}, Reason: "model_usage_recorded",
				}))
			},
		})
		if streamedContent.Len() > 0 {
			// This is exactly what the live stream displayed, even if an adapter
			// omitted the partial ChatResult while returning its error.
			result.Content = streamedContent.String()
		}
		if result.Content != "" || result.ReasoningContent != "" || len(result.ToolCalls) > 0 {
			attemptProducedOutput = true
		}
		if lifecycleErr := getLifecycleErr(); lifecycleErr != nil {
			if !o.executionBoundaryOpen(ctx) {
				return result, context.Canceled
			}
			return result, fmt.Errorf("record model usage: %w", lifecycleErr)
		}
		if err == nil {
			return result, nil
		}
		if errors.Is(err, context.Canceled) || attempt >= o.modelRetryLimit || attemptProducedOutput || !isTransientModelError(err) || retryAfterTooLong(err) {
			return result, err
		}
		// attempt is zero-based for the current request; backoff is keyed by
		// the request number that will follow this retry (2, then 3, ...).
		nextRetryDelay = retryDelayFor(err, attempt+2)
		if lifecycleErr := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
			Type: CommandModelRequestRetrying, At: time.Now().UTC(),
			RequestDigest: requestDigest, ErrorKind: modelErrorKind(err), Retryable: true, RetryAfterMS: nextRetryDelay.Milliseconds(),
			Reason: "transient_model_error",
		}); lifecycleErr != nil {
			return result, fmt.Errorf("record model retry: %w", lifecycleErr)
		}
	}
}

func modelRetryBackoff(attempt int) time.Duration {
	// attempt is the next request number (2, 3, ...). Add up to 25% jitter.
	base := 2 * time.Second * time.Duration(1<<max(0, attempt-2))
	if base > 8*time.Second {
		base = 8 * time.Second
	}
	return base + time.Duration(rand.Int64N(int64(base/4)+1))
}

func retryDelayFor(err error, attempt int) time.Duration {
	var providerErr *llm.ProviderHTTPError
	if errors.As(err, &providerErr) && providerErr.RetryAfter > 0 {
		return providerErr.RetryAfter
	}
	return modelRetryBackoff(attempt)
}

const maxAutomaticRetryAfter = 30 * time.Second

func retryAfterTooLong(err error) bool {
	var providerErr *llm.ProviderHTTPError
	return errors.As(err, &providerErr) && providerErr.RetryAfter > maxAutomaticRetryAfter
}

func isTransientModelError(err error) bool {
	if err == nil {
		return false
	}
	switch llm.ProviderErrorCode(err) {
	case "provider_timeout", "provider_rate_limited", "provider_unavailable", "provider_network_error":
		return true
	default:
		return false
	}
}
