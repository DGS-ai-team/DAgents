package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// OpenAIConfig 为 OpenAI 兼容客户端配置。
type OpenAIConfig struct {
	BaseURL      string
	Model        string
	APIKey       string
	RequestExtra map[string]any // 合并进 POST /chat/completions JSON（如 DeepSeek thinking）
}

// OpenAIClient 通过 HTTP 调用 Chat Completions（stream=true）。
type OpenAIClient struct {
	cfg    OpenAIConfig
	client *http.Client
}

// NewOpenAIClient 构造 OpenAI 兼容客户端。
func NewOpenAIClient(cfg OpenAIConfig) *OpenAIClient {
	base := normalizeOpenAIBaseURL(cfg.BaseURL)
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return &OpenAIClient{
		cfg: OpenAIConfig{
			BaseURL:      base,
			Model:        cfg.Model,
			APIKey:       cfg.APIKey,
			RequestExtra: cfg.RequestExtra,
		},
		client: &http.Client{
			Timeout: 0,
		},
	}
}

type chatRequest struct {
	Model         string           `json:"model"`
	Messages      []map[string]any `json:"messages"`
	Stream        bool             `json:"stream"`
	Tools         []tools.ToolDef  `json:"tools,omitempty"`
	StreamOptions *streamOptions   `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type streamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ReasoningDetails []struct {
				Text string `json:"text"`
			} `json:"reasoning_details"`
			ToolCalls []streamToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// StreamChat 调用 POST /chat/completions 并解析 SSE（含 tool_calls 增量合并）。
// On context cancellation it may return an incomplete ChatResult. That result
// is a live draft for the caller and must never be persisted as model history.
func (c *OpenAIClient) StreamChat(ctx context.Context, req ChatRequest, handler StreamHandler) (ChatResult, error) {
	if strings.TrimSpace(c.cfg.Model) == "" {
		return ChatResult{}, fmt.Errorf("llm model is not configured")
	}
	if strings.TrimSpace(c.cfg.APIKey) == "" {
		return ChatResult{}, fmt.Errorf("llm api key is not configured")
	}

	var body []byte
	var err error
	if len(req.APIMessages) > 0 {
		if err := validateAPIMessages(req.APIMessages); err != nil {
			return ChatResult{}, err
		}
		body, err = marshalChatRequestMap(map[string]any{
			"model":          c.cfg.Model,
			"messages":       req.APIMessages,
			"stream":         true,
			"tools":          req.Tools,
			"stream_options": &streamOptions{IncludeUsage: true},
		}, c.cfg.RequestExtra)
	} else {
		msgs := MessagesWithSystem(req.SystemPrompt, req.Messages)
		payloads, payloadErr := MessagesToAPIPayload(msgs)
		if payloadErr != nil {
			return ChatResult{}, payloadErr
		}
		body, err = marshalChatRequest(chatRequest{
			Model:         c.cfg.Model,
			Messages:      payloads,
			Stream:        true,
			Tools:         req.Tools,
			StreamOptions: &streamOptions{IncludeUsage: true},
		}, c.cfg.RequestExtra)
	}
	if err != nil {
		return ChatResult{}, err
	}

	endpoint := chatCompletionsEndpoint(c.cfg.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return ChatResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ChatResult{}, fmt.Errorf("llm http %d: %s (POST %s)", resp.StatusCode, strings.TrimSpace(string(raw)), endpoint)
	}

	var full strings.Builder
	var fullReasoning strings.Builder
	toolAcc := newToolCallAccumulator()
	var finishReason string
	streamComplete := false

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ChatResult{
				Content:          full.String(),
				ReasoningContent: fullReasoning.String(),
				ToolCalls:        toolAcc.aggregate(),
			}, ctx.Err()
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			streamComplete = true
			break
		}
		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				full.WriteString(delta)
				if handler.OnDelta != nil {
					handler.OnDelta(delta)
				}
			}
			reasoning := chunk.Choices[0].Delta.ReasoningContent
			if reasoning != "" {
				fullReasoning.WriteString(reasoning)
				if handler.OnReasoningDelta != nil {
					handler.OnReasoningDelta(reasoning)
				}
			} else if len(chunk.Choices[0].Delta.ReasoningDetails) > 0 {
				// MiniMax may expose reasoning through reasoning_details when
				// reasoning_split is enabled. Some versions send cumulative text,
				// so append only the suffix not already present.
				for _, detail := range chunk.Choices[0].Delta.ReasoningDetails {
					if detail.Text == "" {
						continue
					}
					newReasoning := appendReasoningDetail(&fullReasoning, detail.Text)
					if newReasoning != "" && handler.OnReasoningDelta != nil {
						handler.OnReasoningDelta(newReasoning)
					}
				}
			}
			for _, tc := range chunk.Choices[0].Delta.ToolCalls {
				toolAcc.add(tc)
			}
			if handler.OnToolCallDelta != nil {
				if snap := toolAcc.snapshot(); len(snap) > 0 {
					handler.OnToolCallDelta(snap)
				}
			}
			if chunk.Choices[0].FinishReason != nil {
				finishReason = *chunk.Choices[0].FinishReason
			}
		}
		if chunk.Usage != nil && handler.OnUsage != nil {
			handler.OnUsage(*chunk.Usage)
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatResult{
			Content:          full.String(),
			ReasoningContent: fullReasoning.String(),
			ToolCalls:        toolAcc.aggregate(),
		}, err
	}
	if !streamComplete && strings.TrimSpace(finishReason) == "" {
		return ChatResult{
			Content:          full.String(),
			ReasoningContent: fullReasoning.String(),
			ToolCalls:        toolAcc.aggregate(),
		}, fmt.Errorf("llm stream ended before completion: %w", io.ErrUnexpectedEOF)
	}

	tcs := toolAcc.aggregate()
	if finishReason == "" {
		if len(tcs) > 0 {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}
	result := ChatResult{
		Content:          full.String(),
		ReasoningContent: fullReasoning.String(),
		ToolCalls:        tcs,
		FinishReason:     finishReason,
	}
	if err := ValidateAssistantMessage(Message{
		Role:             "assistant",
		Content:          result.Content,
		ReasoningContent: result.ReasoningContent,
		ToolCalls:        result.ToolCalls,
	}); err != nil {
		// The provider stream ended normally, so preserve the typed protocol
		// error for diagnostics but never let the caller treat this response as
		// an executable assistant message.
		return result, fmt.Errorf("invalid provider tool call: %w", err)
	}
	return result, nil
}

// validateAPIMessages closes the validation gap for adapters that already
// serialized messages into the final provider shape. Content is deliberately
// ignored here (it may be a string or multimodal array); tool protocol fields
// are decoded into the shared internal representation and validated once.
func validateAPIMessages(payloads []map[string]any) error {
	if len(payloads) == 0 {
		return nil
	}
	type apiMessage struct {
		Role       string     `json:"role"`
		ToolCalls  []ToolCall `json:"tool_calls"`
		ToolCallID string     `json:"tool_call_id"`
	}
	messages := make([]Message, len(payloads))
	for index, payload := range payloads {
		raw, err := json.Marshal(payload)
		if err != nil {
			return &HistoryValidationError{Violations: []HistoryViolation{{
				Code:         "api_message_invalid",
				MessageIndex: index,
			}}}
		}
		var parsed apiMessage
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return &HistoryValidationError{Violations: []HistoryViolation{{
				Code:         "api_message_invalid",
				MessageIndex: index,
				Detail:       "provider message fields are malformed",
			}}}
		}
		messages[index] = Message{
			Role:       parsed.Role,
			ToolCalls:  parsed.ToolCalls,
			ToolCallID: parsed.ToolCallID,
		}
	}
	return ValidateToolProtocol(messages)
}

func appendReasoningDetail(full *strings.Builder, detail string) string {
	if full == nil || detail == "" {
		return ""
	}
	current := full.String()
	switch {
	case strings.HasPrefix(detail, current):
		suffix := detail[len(current):]
		full.WriteString(suffix)
		return suffix
	case strings.HasPrefix(current, detail):
		return ""
	default:
		full.WriteString(detail)
		return detail
	}
}

type completeRequestBody struct {
	Model    string           `json:"model"`
	Messages []map[string]any `json:"messages"`
}

type completeResponseBody struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// CompleteText 调用非流式 chat/completions（摘要压缩等）。
func (c *OpenAIClient) CompleteText(ctx context.Context, req CompleteRequest) (string, error) {
	if strings.TrimSpace(c.cfg.Model) == "" {
		return "", fmt.Errorf("llm model is not configured")
	}
	if strings.TrimSpace(c.cfg.APIKey) == "" {
		return "", fmt.Errorf("llm api key is not configured")
	}
	msgs := MessagesWithSystem(req.SystemPrompt, []Message{{Role: "user", Content: req.UserPrompt}})
	payloads, err := MessagesToAPIPayload(msgs)
	if err != nil {
		return "", err
	}
	body, err := marshalChatRequest(completeRequestBody{Model: c.cfg.Model, Messages: payloads}, c.cfg.RequestExtra)
	if err != nil {
		return "", err
	}
	endpoint := chatCompletionsEndpoint(c.cfg.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm http %d: %s (POST %s)", resp.StatusCode, strings.TrimSpace(string(raw)), endpoint)
	}
	var parsed completeResponseBody
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty completion choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

type toolCallAccumulator struct {
	byIndex map[int]*ToolCall
	order   []int
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{byIndex: make(map[int]*ToolCall)}
}

func (a *toolCallAccumulator) add(delta streamToolCallDelta) {
	tc, ok := a.byIndex[delta.Index]
	if !ok {
		tc = &ToolCall{Type: "function"}
		a.byIndex[delta.Index] = tc
		a.order = append(a.order, delta.Index)
	}
	if delta.ID != "" {
		tc.ID = delta.ID
	}
	if delta.Type != "" {
		tc.Type = delta.Type
	}
	if delta.Function.Name != "" {
		tc.Function.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		tc.Function.Arguments += delta.Function.Arguments
	}
}

func (a *toolCallAccumulator) snapshot() []ToolCall {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		tc := a.byIndex[idx]
		if tc == nil {
			continue
		}
		out = append(out, *tc)
	}
	return out
}

// aggregate returns the accumulated tool-call snapshot after the stream has
// ended. It does not validate JSON arguments; the caller must decide whether
// the stream ended successfully and then run ValidateAssistantMessage.
func (a *toolCallAccumulator) aggregate() []ToolCall {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		if tc := a.byIndex[idx]; tc != nil && tc.Function.Name != "" {
			out = append(out, *tc)
		}
	}
	return out
}

func marshalChatRequest(body any, extra map[string]any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return mergeRequestExtra(raw, extra)
}

func marshalChatRequestMap(body map[string]any, extra map[string]any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return mergeRequestExtra(raw, extra)
}

func mergeRequestExtra(raw []byte, extra map[string]any) ([]byte, error) {
	if len(extra) == 0 {
		return raw, nil
	}
	var merged map[string]any
	if err := json.Unmarshal(raw, &merged); err != nil {
		return nil, err
	}
	for k, v := range extra {
		merged[k] = v
	}
	return json.Marshal(merged)
}
