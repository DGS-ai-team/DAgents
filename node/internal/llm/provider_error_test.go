package llm

import (
	"context"
	"io"
	"net"
	"testing"
)

func TestIsTransientProviderNetworkErrorUsesExplicitTransientSignals(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "temporary DNS failure", err: &net.DNSError{Err: "server failure", IsTemporary: true}, want: true},
		{name: "DNS timeout", err: &net.DNSError{Err: "timeout", IsTimeout: true}, want: true},
		{name: "permanent DNS failure", err: &net.DNSError{Err: "not found", IsNotFound: true}, want: false},
		{name: "timeout network error", err: providerTestNetworkError{timeout: true}, want: true},
		{name: "non-timeout network error", err: providerTestNetworkError{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientProviderNetworkError(tt.err); got != tt.want {
				t.Fatalf("isTransientProviderNetworkError(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}

type providerTestNetworkError struct {
	timeout bool
}

func (e providerTestNetworkError) Error() string   { return "network failure" }
func (e providerTestNetworkError) Timeout() bool   { return e.timeout }
func (e providerTestNetworkError) Temporary() bool { return true }

var _ net.Error = providerTestNetworkError{}

func TestProviderErrorCodeClassifiesTimeoutSeparatelyFromEOF(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "context deadline", err: context.DeadlineExceeded, want: "provider_timeout"},
		{name: "network timeout", err: providerTestNetworkError{timeout: true}, want: "provider_timeout"},
		{name: "ordinary EOF", err: io.EOF, want: "provider_network_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProviderErrorCode(tt.err); got != tt.want {
				t.Fatalf("ProviderErrorCode(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
