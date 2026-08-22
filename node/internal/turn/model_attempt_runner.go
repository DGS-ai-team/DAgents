package turn

import (
	"context"
	"errors"
	"fmt"
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
	for attempt := 0; ; attempt++ {
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
			if retryErr := waitModelRetry(ctx, attempt); retryErr != nil {
				return result, retryErr
			}
		}
		if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
			Type: CommandModelRequestStarted, At: time.Now().UTC(),
			RequestDigest: requestDigest, Reason: "model_request_started",
		}); err != nil {
			return result, fmt.Errorf("record model request start: %w", err)
		}
		attemptProducedOutput := false
		result, err = o.llm.StreamChat(ctx, llm.ChatRequest{
			SystemPrompt: systemPrompt,
			Messages:     messages,
			Tools:        toolDefs,
		}, llm.StreamHandler{
			OnDelta: func(delta string) {
				if strings.TrimSpace(delta) != "" {
					attemptProducedOutput = true
				}
				o.publishAssistant(sessionID, delta)
			},
			OnReasoningDelta: func(delta string) {
				if strings.TrimSpace(delta) != "" {
					attemptProducedOutput = true
				}
				o.publishReasoning(sessionID, delta)
			},
			OnToolCallDelta: func(calls []llm.ToolCall) {
				for i, tc := range calls {
					if strings.TrimSpace(tc.Function.Name) == "" {
						continue
					}
					attemptProducedOutput = true
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
		if lifecycleErr := getLifecycleErr(); lifecycleErr != nil {
			return result, fmt.Errorf("record model usage: %w", lifecycleErr)
		}
		if err == nil {
			return result, nil
		}
		if errors.Is(err, context.Canceled) || attempt >= o.modelRetryLimit || attemptProducedOutput || !isTransientModelError(err) {
			return result, err
		}
		if lifecycleErr := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
			Type: CommandModelRequestRetrying, At: time.Now().UTC(),
			RequestDigest: requestDigest, ErrorKind: modelErrorKind(err),
			Reason: "transient_model_error",
		}); lifecycleErr != nil {
			return result, fmt.Errorf("record model retry: %w", lifecycleErr)
		}
	}
}
