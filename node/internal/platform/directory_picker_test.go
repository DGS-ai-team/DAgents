package platform

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandDirectoryPickerAvailableDoesNotOpenDialog(t *testing.T) {
	command, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go command is unavailable: %v", err)
	}
	picker := &commandDirectoryPicker{resolve: func() (string, []string, error) {
		return command, []string{"version"}, nil
	}}
	if !picker.Available(context.Background()) {
		t.Fatal("Available should only inspect the resolver and not require launching a dialog")
	}
}

func TestCommandDirectoryPickerReturnsUnavailableWithoutResolver(t *testing.T) {
	var picker *commandDirectoryPicker
	_, err := picker.Pick(context.Background())
	if !errors.Is(err, ErrDirectoryPickerUnavailable) {
		t.Fatalf("err=%v, want ErrDirectoryPickerUnavailable", err)
	}
}

func TestCommandDirectoryPickerReturnsSelectedPath(t *testing.T) {
	picker := &commandDirectoryPicker{resolve: func() (string, []string, error) {
		return "go", []string{"env", "GOROOT"}, nil
	}}
	got, err := picker.Pick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expectedRaw, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.TrimSpace(string(expectedRaw))
	if !got.OK || got.Cancelled || got.Path != expected {
		t.Fatalf("result=%+v", got)
	}
}
