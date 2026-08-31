//go:build windows

package computeruse

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	procSetCursor  = user32.NewProc("SetCursorPos")
	procMouseEvent = user32.NewProc("mouse_event")
	procSendInput  = user32.NewProc("SendInput")
)

const (
	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800
	keyEventKeyUp   = 0x0002
	keyEventUnicode = 0x0004
)

type windowsBackend struct{}

func newBackend() Backend { return windowsBackend{} }

func (windowsBackend) Status() Status {
	return Status{Available: true, Backend: "windows-sendinput"}
}

func (windowsBackend) Execute(ctx context.Context, action Action) error {
	if err := action.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(action.Name)) {
	case ActionMove:
		return setCursor(action.X, action.Y)
	case ActionClick, ActionDoubleClick:
		if err := setCursor(action.X, action.Y); err != nil {
			return err
		}
		count := 1
		if action.Name == ActionDoubleClick {
			count = 2
		}
		for i := 0; i < count; i++ {
			if err := clickMouse(action.Button); err != nil {
				return err
			}
		}
		return nil
	case ActionDrag:
		if err := setCursor(action.FromX, action.FromY); err != nil {
			return err
		}
		down, up := mouseButtonFlags(action.Button)
		procMouseEvent.Call(uintptr(down), 0, 0, 0, 0)
		if err := setCursor(action.X, action.Y); err != nil {
			procMouseEvent.Call(uintptr(up), 0, 0, 0, 0)
			return err
		}
		procMouseEvent.Call(uintptr(up), 0, 0, 0, 0)
		return nil
	case ActionScroll:
		if action.HasPoint {
			if err := setCursor(action.X, action.Y); err != nil {
				return err
			}
		}
		delta := int32(-action.ScrollY * 120)
		procMouseEvent.Call(mouseWheel, 0, 0, uintptr(uint32(delta)), 0)
		return nil
	case ActionKey:
		return pressKey(action.Key, action.Modifiers)
	case ActionTypeText:
		return typeUnicode(ctx, action.Text)
	default:
		return fmt.Errorf("unsupported computer action %q", action.Name)
	}
}

func setCursor(x, y int) error {
	ok, _, err := procSetCursor.Call(uintptr(int32(x)), uintptr(int32(y)))
	if ok == 0 {
		return fmt.Errorf("SetCursorPos: %w", err)
	}
	return nil
}

func mouseButtonFlags(button string) (uint32, uint32) {
	switch strings.ToLower(strings.TrimSpace(button)) {
	case "right":
		return mouseRightDown, mouseRightUp
	case "middle":
		return mouseMiddleDown, mouseMiddleUp
	default:
		return mouseLeftDown, mouseLeftUp
	}
}

func clickMouse(button string) error {
	down, up := mouseButtonFlags(button)
	procMouseEvent.Call(uintptr(down), 0, 0, 0, 0)
	procMouseEvent.Call(uintptr(up), 0, 0, 0, 0)
	return nil
}

func pressKey(key string, modifiers []string) error {
	modifierKeys := make([]uint16, 0, len(modifiers))
	for _, modifier := range modifiers {
		vk, ok := virtualKey(modifier)
		if !ok || (vk != 0x10 && vk != 0x11 && vk != 0x12 && vk != 0x5B) {
			return fmt.Errorf("unsupported modifier %q", modifier)
		}
		modifierKeys = append(modifierKeys, vk)
	}
	vk, ok := virtualKey(key)
	if !ok {
		return fmt.Errorf("unsupported key %q", key)
	}
	pressed := make([]uint16, 0, len(modifierKeys)+1)
	defer func() {
		for i := len(pressed) - 1; i >= 0; i-- {
			_ = sendVirtualKey(pressed[i], true)
		}
	}()
	for _, modifier := range modifierKeys {
		if err := sendVirtualKey(modifier, false); err != nil {
			return err
		}
		pressed = append(pressed, modifier)
	}
	if err := sendVirtualKey(vk, false); err != nil {
		return err
	}
	pressed = append(pressed, vk)
	if err := sendVirtualKey(vk, true); err != nil {
		return err
	}
	pressed = pressed[:len(pressed)-1]
	for i := len(modifierKeys) - 1; i >= 0; i-- {
		if err := sendVirtualKey(modifierKeys[i], true); err != nil {
			return err
		}
		pressed = pressed[:len(pressed)-1]
	}
	return nil
}

func virtualKey(raw string) (uint16, bool) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if len(key) == 1 {
		c := key[0]
		if c >= 'a' && c <= 'z' {
			return uint16(c - 'a' + 'A'), true
		}
		if c >= '0' && c <= '9' {
			return uint16(c), true
		}
	}
	keys := map[string]uint16{
		"backspace": 0x08, "tab": 0x09, "enter": 0x0D, "shift": 0x10,
		"ctrl": 0x11, "control": 0x11, "alt": 0x12, "escape": 0x1B, "esc": 0x1B,
		"space": 0x20, "pageup": 0x21, "pagedown": 0x22, "end": 0x23, "home": 0x24,
		"left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28, "delete": 0x2E,
		"meta": 0x5B, "win": 0x5B, "cmd": 0x5B,
		"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74, "f6": 0x75,
		"f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
	}
	vk, ok := keys[key]
	return vk, ok
}

func typeUnicode(ctx context.Context, text string) error {
	for _, codeUnit := range utf16.Encode([]rune(text)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sendKeyboardInput(0, codeUnit, keyEventUnicode); err != nil {
			return err
		}
		if err := sendKeyboardInput(0, codeUnit, keyEventUnicode|keyEventKeyUp); err != nil {
			return err
		}
	}
	return nil
}

func sendVirtualKey(vk uint16, up bool) error {
	flags := uint32(0)
	if up {
		flags = keyEventKeyUp
	}
	return sendKeyboardInput(vk, 0, flags)
}

func sendKeyboardInput(vk, scan uint16, flags uint32) error {
	pointerSize := unsafe.Sizeof(uintptr(0))
	inputSize := uintptr(28)
	unionOffset := 4
	if pointerSize == 8 {
		inputSize = 40
		unionOffset = 8
	}
	input := make([]byte, inputSize)
	binary.LittleEndian.PutUint32(input[0:4], 1)
	binary.LittleEndian.PutUint16(input[unionOffset:unionOffset+2], vk)
	binary.LittleEndian.PutUint16(input[unionOffset+2:unionOffset+4], scan)
	binary.LittleEndian.PutUint32(input[unionOffset+4:unionOffset+8], flags)
	count, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&input[0])), inputSize)
	if count != 1 {
		return fmt.Errorf("SendInput: %w", err)
	}
	return nil
}
