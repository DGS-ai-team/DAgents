# Security logging rules

Logs are operational telemetry, not a copy of request or tool data.

- Never log API keys, authorization headers, cookies, raw query values, full
  tool arguments, command output, message content, or uploaded file contents.
- Prefer stable identifiers, sizes, counts, status, duration, and a sanitized
  error category. The HTTP access logger records query parameter names only.
- Treat third-party error strings as untrusted: they can contain URLs,
  request bodies, or credentials. Review new error logging at the call site.
- If debugging requires sensitive data, use a local reproduction or an
  explicitly gated redaction-safe diagnostic rather than raising production
  log verbosity.

Security-sensitive logging changes should be called out in the PR and covered
by a test that asserts secrets do not reach the logger output.
