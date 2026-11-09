package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"time"
)

// ProviderHTTPError retains provider diagnostics for logs while exposing a
// status code and retry delay that callers can classify without parsing text.
type ProviderHTTPError struct {
	StatusCode int
	RetryAfter time.Duration
	Body       string
	Endpoint   string
}

func (e *ProviderHTTPError) Error() string {
	if e == nil {
		return "provider HTTP error"
	}
	return fmt.Sprintf("llm http %d: %s (POST %s)", e.StatusCode, e.Body, e.Endpoint)
}

// UserMessage is intentionally independent from the raw provider body, which
// may contain request details or other diagnostics unsuitable for the UI.
func (e *ProviderHTTPError) UserMessage() string {
	if e == nil {
		return "模型服务暂时不可用，请稍后重试。"
	}
	return ProviderErrorMessageForCode(ProviderErrorCode(e))
}

// ProviderErrorCode returns the stable, safe category shared by lifecycle
// events, hydrate projections, and UI error summaries.
func ProviderErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var providerErr *ProviderHTTPError
	if errors.As(err, &providerErr) {
		if code := providerErrorCode(providerErr.Body); code != "" {
			return code
		}
		switch providerErr.StatusCode {
		case 401:
			return "provider_authentication"
		case 402:
			return "provider_quota"
		case 408:
			return "provider_timeout"
		case 429:
			return "provider_rate_limited"
		case 400, 403, 404, 422:
			return "provider_configuration"
		case 500, 502, 503, 504:
			return "provider_unavailable"
		default:
			return "provider_http_error"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || isProviderTimeoutError(err) {
		return "provider_timeout"
	}
	if isTransientProviderNetworkError(err) {
		return "provider_network_error"
	}
	return "provider_error"
}

// providerErrorCode reads only explicit structured provider fields. Provider
// message text is intentionally excluded because it is free-form and may
// contain request details; it must not control retry or quota classification.
func providerErrorCode(body string) string {
	var envelope struct {
		Code  string          `json:"code"`
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(body), &envelope) != nil {
		return ""
	}
	fields := []string{envelope.Code, envelope.Type}
	if len(envelope.Error) > 0 {
		var nested struct {
			Code string `json:"code"`
			Type string `json:"type"`
		}
		if json.Unmarshal(envelope.Error, &nested) == nil {
			fields = append(fields, nested.Code, nested.Type)
		}
	}
	for _, field := range fields {
		switch normalizeProviderErrorCode(field) {
		case "insufficient_quota", "quota_exceeded", "billing_hard_limit_reached", "account_insufficient_quota":
			return "provider_quota"
		case "invalid_api_key", "incorrect_api_key", "invalid_token", "authentication_error", "unauthorized":
			return "provider_authentication"
		case "rate_limit_exceeded", "too_many_requests", "rate_limited":
			return "provider_rate_limited"
		case "request_timeout", "timeout":
			return "provider_timeout"
		case "server_error", "internal_error", "service_unavailable", "overloaded", "upstream_error":
			return "provider_unavailable"
		}
	}
	return ""
}

func normalizeProviderErrorCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "_")
	return code
}

func isProviderTimeoutError(err error) bool {
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}

// ProviderErrorUserMessage returns a provider-safe summary without exposing
// response bodies, endpoint URLs, credentials, or transport diagnostics.
func ProviderErrorUserMessage(err error) string {
	if err == nil {
		return "模型请求失败，请稍后重试。"
	}
	return ProviderErrorMessageForCode(ProviderErrorCode(err))
}

// ProviderErrorMessageForCode is the hydrate-safe counterpart used when the
// durable event stores only the structured error code.
func ProviderErrorMessageForCode(code string) string {
	switch code {
	case "provider_authentication":
		return "模型服务认证失败，请检查 API 密钥。"
	case "provider_quota":
		return "模型服务账户余额不足或额度已用完。"
	case "provider_rate_limited":
		return "模型服务请求过于频繁，请稍后重试。"
	case "provider_timeout":
		return "模型服务响应超时，请稍后重试。"
	case "provider_configuration":
		return "模型请求或配置无效，请检查模型名称、接口地址和请求参数。"
	case "provider_network_error":
		return "连接模型服务失败，请检查网络后重试。"
	case "provider_unavailable", "transient_provider_error":
		return "模型服务暂时不可用，请稍后重试。"
	case "provider_http_error", "provider_error":
		return "模型请求失败，请检查模型配置后重试。"
	default:
		return "模型请求失败，请稍后重试。"
	}
}

func isTransientProviderNetworkError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	// DNS errors expose transient and timeout conditions directly. Avoid the
	// deprecated net.Error.Temporary method, whose semantics are not defined.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && (dnsErr.IsTemporary || dnsErr.IsTimeout) {
		return true
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}
