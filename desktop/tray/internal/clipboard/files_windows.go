//go:build windows

package clipboard

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cfHDrop = 15

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	shell32            = windows.NewLazySystemDLL("shell32.dll")
	procOpenClipboard  = user32.NewProc("OpenClipboard")
	procCloseClipboard = user32.NewProc("CloseClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procDragQuery      = shell32.NewProc("DragQueryFileW")
)

func filePaths() ([]string, error) {
	if err := openClipboard(); err != nil {
		return nil, fmt.Errorf("open clipboard: %w", err)
	}
	defer closeClipboard()

	h, err := getClipboardData(cfHDrop)
	if err != nil || h == 0 {
		return nil, nil
	}
	return pathsFromHDROP(h)
}

func openClipboard() error {
	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
		if err != windows.ERROR_SUCCESS {
			return err
		}
		return errors.New("OpenClipboard failed")
	}
	return nil
}

func closeClipboard() {
	_, _, _ = procCloseClipboard.Call()
}

func getClipboardData(format uint32) (windows.Handle, error) {
	r, _, err := procGetClipboardData.Call(uintptr(format))
	if r == 0 {
		if err != windows.ERROR_SUCCESS {
			return 0, err
		}
		return 0, nil
	}
	return windows.Handle(r), nil
}

func pathsFromHDROP(h windows.Handle) ([]string, error) {
	count, _, _ := procDragQuery.Call(uintptr(h), ^uintptr(0), 0, 0)
	if count == 0 {
		return nil, nil
	}
	paths := make([]string, 0, count)
	for i := uintptr(0); i < count; i++ {
		n, _, _ := procDragQuery.Call(uintptr(h), i, 0, 0)
		if n == 0 {
			continue
		}
		buf := make([]uint16, n+1)
		written, _, _ := procDragQuery.Call(
			uintptr(h),
			i,
			uintptr(unsafe.Pointer(&buf[0])),
			n+1,
		)
		if written == 0 {
			continue
		}
		paths = append(paths, windows.UTF16ToString(buf[:written]))
	}
	return paths, nil
}
