//go:build linux

package computeruse

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type linuxBackend struct {
	path   string
	status Status
}

func newBackend() Backend {
	if strings.TrimSpace(os.Getenv("DISPLAY")) == "" {
		return linuxBackend{status: Status{Backend: "none", Reason: "x11_display_unavailable"}}
	}
	path, err := exec.LookPath("xdotool")
	if err != nil {
		return linuxBackend{status: Status{Backend: "none", Reason: "xdotool_not_installed"}}
	}
	return linuxBackend{path: path, status: Status{Available: true, Backend: "linux-xdotool"}}
}

func (b linuxBackend) Status() Status { return b.status }

func (b linuxBackend) Execute(ctx context.Context, action Action) error {
	if err := action.Validate(); err != nil {
		return err
	}
	if !b.status.Available || b.path == "" {
		return fmt.Errorf("computer use unavailable: %s", b.status.Reason)
	}
	point := func(x, y int) []string { return []string{"mousemove", strconv.Itoa(x), strconv.Itoa(y)} }
	run := func(args ...string) error {
		out, err := exec.CommandContext(ctx, b.path, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("xdotool %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(action.Name)) {
	case ActionMove:
		return run(point(action.X, action.Y)...)
	case ActionClick, ActionDoubleClick:
		if err := run(point(action.X, action.Y)...); err != nil {
			return err
		}
		args := []string{"click"}
		if action.Name == ActionDoubleClick {
			args = append(args, "--repeat", "2", "--delay", "100")
		}
		return run(append(args, mouseButtonNumber(action.Button))...)
	case ActionDrag:
		if err := run(point(action.FromX, action.FromY)...); err != nil {
			return err
		}
		button := mouseButtonNumber(action.Button)
		if err := run("mousedown", button); err != nil {
			return err
		}
		if err := run(point(action.X, action.Y)...); err != nil {
			_ = run("mouseup", button)
			return err
		}
		return run("mouseup", button)
	case ActionScroll:
		if action.HasPoint {
			if err := run(point(action.X, action.Y)...); err != nil {
				return err
			}
		}
		button := "5"
		amount := action.ScrollY
		if amount < 0 {
			button = "4"
			amount = -amount
		}
		return run("click", "--repeat", strconv.Itoa(amount), button)
	case ActionKey:
		parts := make([]string, 0, len(action.Modifiers)+1)
		for _, modifier := range action.Modifiers {
			parts = append(parts, xdotoolKeyName(modifier))
		}
		parts = append(parts, xdotoolKeyName(action.Key))
		return run("key", "--clearmodifiers", strings.Join(parts, "+"))
	case ActionTypeText:
		return run("type", "--clearmodifiers", "--delay", "1", "--", action.Text)
	default:
		return fmt.Errorf("unsupported computer action %q", action.Name)
	}
}

func mouseButtonNumber(button string) string {
	switch strings.ToLower(strings.TrimSpace(button)) {
	case "right":
		return "3"
	case "middle":
		return "2"
	default:
		return "1"
	}
}

func xdotoolKeyName(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "ctrl", "control":
		return "ctrl"
	case "meta", "win", "cmd":
		return "super"
	case "escape":
		return "Escape"
	case "pageup":
		return "Page_Up"
	case "pagedown":
		return "Page_Down"
	default:
		return key
	}
}
