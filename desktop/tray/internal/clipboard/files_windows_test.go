//go:build windows

package clipboard

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestPathsFromHDROPEmpty(t *testing.T) {
	paths, err := pathsFromHDROP(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty, got %v", paths)
	}
}

func TestPathsFromHDROPInvalidHandle(t *testing.T) {
	// Invalid handle should not panic; DragQueryFile returns 0 files.
	paths, err := pathsFromHDROP(windows.Handle(^uintptr(0)))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty, got %v", paths)
	}
}

func TestDragQueryFileRoundTrip(t *testing.T) {
	// Sanity: proc resolves on Windows.
	if procDragQuery.Find() != nil {
		t.Fatal("DragQueryFileW not found")
	}
	_ = unsafe.Pointer(nil)
}
