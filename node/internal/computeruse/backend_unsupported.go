//go:build !windows && !linux

package computeruse

import (
	"context"
	"fmt"
)

type unsupportedBackend struct{}

func newBackend() Backend { return unsupportedBackend{} }
func (unsupportedBackend) Status() Status {
	return Status{Backend: "none", Reason: "unsupported_os"}
}
func (unsupportedBackend) Execute(context.Context, Action) error {
	return fmt.Errorf("computer use unavailable: unsupported OS")
}
