//go:build windows

package platform

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestResolveWindowsDirectoryPickerUsesSTAFolderBrowserDialog(t *testing.T) {
	command, args, err := resolveWindowsDirectoryPicker()
	if err != nil {
		t.Skipf("PowerShell is unavailable on this host: %v", err)
	}
	if !strings.HasSuffix(strings.ToLower(command), "powershell.exe") && !strings.HasSuffix(strings.ToLower(command), "pwsh.exe") {
		t.Fatalf("command=%q", command)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-STA") || !strings.Contains(joined, "-EncodedCommand") {
		t.Fatalf("args=%v", args)
	}
	encoded := args[len(args)-1]
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	if !strings.Contains(string(utf16.Decode(units)), "FolderBrowserDialog") {
		t.Fatalf("script does not use FolderBrowserDialog")
	}
}
