package workgroup

import "fmt"

// ErrorCode 对齐 D0.5 §7.4。
type ErrorCode string

const (
	CodeNotAuthorized     ErrorCode = "not_authorized"
	CodeFencingRejected   ErrorCode = "fencing_rejected"
	CodeDigestMismatch    ErrorCode = "digest_mismatch"
	CodeCatalogDrift      ErrorCode = "catalog_drift"
	CodePayloadConflict   ErrorCode = "payload_conflict"
	CodeAlreadyResolved   ErrorCode = "already_resolved"
	CodePolicyDenied      ErrorCode = "policy_denied"
	CodeSchemaMismatch    ErrorCode = "schema_mismatch"
	CodeIndeterminate     ErrorCode = "indeterminate"
	CodeWorkgroupArchived ErrorCode = "workgroup_archived"
	CodeNotFound          ErrorCode = "not_found"
	CodeConflict          ErrorCode = "conflict"
	CodeCanceled          ErrorCode = "canceled"
)

// Error 为可映射到契约错误码的 Worker 错误。
type Error struct {
	Code      ErrorCode
	Message   string
	Retryable bool
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func errf(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
