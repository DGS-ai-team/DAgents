// Package computeruse provides a small, auditable desktop input backend.
// Policy and screenshot-to-OS coordinate mapping remain in the tool layer.
package computeruse

import (
	"context"
	"fmt"
	"strings"
)

const (
	ActionMove        = "move"
	ActionClick       = "click"
	ActionDoubleClick = "double_click"
	ActionDrag        = "drag"
	ActionScroll      = "scroll"
	ActionKey         = "key"
	ActionTypeText    = "type_text"
)

type Status struct {
	Available bool   `json:"available"`
	Backend   string `json:"backend"`
	Reason    string `json:"reason,omitempty"`
}

type Action struct {
	Name      string
	X         int
	Y         int
	HasPoint  bool
	FromX     int
	FromY     int
	HasFrom   bool
	Button    string
	ScrollY   int
	Key       string
	Modifiers []string
	Text      string
}

func (a Action) Validate() error {
	name := strings.ToLower(strings.TrimSpace(a.Name))
	switch name {
	case ActionMove, ActionClick, ActionDoubleClick:
		if !a.HasPoint {
			return fmt.Errorf("x and y are required for %s", name)
		}
	case ActionDrag:
		if !a.HasPoint || !a.HasFrom {
			return fmt.Errorf("from_x, from_y, x and y are required for drag")
		}
	case ActionScroll:
		if a.ScrollY == 0 || a.ScrollY < -20 || a.ScrollY > 20 {
			return fmt.Errorf("scroll_y must be between -20 and 20 and not zero")
		}
	case ActionKey:
		if strings.TrimSpace(a.Key) == "" {
			return fmt.Errorf("key is required")
		}
	case ActionTypeText:
		if a.Text == "" {
			return fmt.Errorf("text is required")
		}
		if len([]rune(a.Text)) > 4000 {
			return fmt.Errorf("text exceeds 4000 characters")
		}
	default:
		return fmt.Errorf("unsupported computer action %q", a.Name)
	}
	switch strings.ToLower(strings.TrimSpace(a.Button)) {
	case "", "left", "middle", "right":
	default:
		return fmt.Errorf("button must be left, middle or right")
	}
	return nil
}

type Backend interface {
	Status() Status
	Execute(context.Context, Action) error
}

func Default() Backend { return newBackend() }
