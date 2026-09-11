// Package platform contains Node-owned integrations with the local operating
// system. These integrations are intentionally independent of the optional
// Desktop Shell so a browser-only Node can still expose native capabilities.
package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// DirectoryPickResult is the stable response returned by the native directory
// picker. A user cancellation is a successful request with Cancelled=true.
type DirectoryPickResult struct {
	OK        bool   `json:"ok"`
	Cancelled bool   `json:"cancelled"`
	Path      string `json:"path,omitempty"`
}

// DirectoryPicker opens a native directory chooser owned by the Node process.
// Available must be side-effect free: it only checks whether the host has a
// supported picker command/runtime and must never open a dialog.
type DirectoryPicker interface {
	Available(context.Context) bool
	Pick(context.Context) (DirectoryPickResult, error)
}

var ErrDirectoryPickerUnavailable = errors.New("native directory picker is unavailable")

// NewDirectoryPicker returns the host implementation without requiring the
// caller to know which native command or runtime is available.
func NewDirectoryPicker() DirectoryPicker { return newDirectoryPicker() }

type commandDirectoryPicker struct {
	resolve func() (string, []string, error)
}

func (p *commandDirectoryPicker) Available(_ context.Context) bool {
	if p == nil || p.resolve == nil {
		return false
	}
	_, _, err := p.resolve()
	return err == nil
}

func (p *commandDirectoryPicker) Pick(ctx context.Context) (DirectoryPickResult, error) {
	if p == nil || p.resolve == nil {
		return DirectoryPickResult{}, ErrDirectoryPickerUnavailable
	}
	path, args, err := p.resolve()
	if err != nil {
		return DirectoryPickResult{}, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	configureDirectoryPickerCommand(cmd)
	output, err := cmd.Output()
	if err != nil {
		// zenity, kdialog, yad and osascript use exit status 1 when the user
		// closes/cancels the dialog. Treat that as a normal cancelled result.
		if isPickerCancellation(err) {
			return DirectoryPickResult{OK: true, Cancelled: true}, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return DirectoryPickResult{}, ctxErr
		}
		return DirectoryPickResult{}, fmt.Errorf("native directory picker: %w", err)
	}
	selected := strings.TrimSpace(string(output))
	if selected == "" {
		return DirectoryPickResult{OK: true, Cancelled: true}, nil
	}
	return DirectoryPickResult{OK: true, Path: selected}, nil
}

func isPickerCancellation(err error) bool {
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	status, ok := exitErr.Sys().(interface{ ExitStatus() int })
	return ok && status.ExitStatus() == 1
}
