package tools

import (
	"bytes"
	"encoding/base64"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestWrapShellCommandForPipe(t *testing.T) {
	in := "Write-Output 中文"
	got := wrapShellCommandForPipe(shellPowerShell, in)
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(got, powerShellPipeEncodingPrefix) {
			t.Fatalf("got %q", got)
		}
		if !strings.HasSuffix(got, in) {
			t.Fatalf("suffix missing: %q", got)
		}
		return
	}
	if got != in {
		t.Fatalf("non-windows should pass through: %q", got)
	}
}

func TestWrapShellCommandForPipe_otherShells(t *testing.T) {
	in := "echo ok"
	for _, st := range []shellType{shellBash, shellCmd} {
		if got := wrapShellCommandForPipe(st, in); got != in {
			t.Fatalf("%s: got %q", st, got)
		}
	}
}

func TestEncodePowerShellCommandRoundTrip(t *testing.T) {
	in := `$payload = '{"path":"C:\\临时\"文件\"","value":"$HOME ` + "`" + `tick"}'; Write-Output $payload 😀`
	encoded := encodePowerShellCommand(in)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("UTF-16LE payload has odd length: %d", len(raw))
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	if got := string(utf16.Decode(units)); got != in {
		t.Fatalf("round trip mismatch:\n got: %q\nwant: %q", got, in)
	}
}

func TestBuildPowerShellCommandUsesEncodedCommand(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell command construction is platform-specific")
	}
	in := `Write-Output "quotes 'single' $env:PATH ` + "`" + `tick"; Write-Output '中文 😀'`
	cmd, err := buildShellCommand(shellPowerShell, in)
	if err != nil {
		t.Fatal(err)
	}
	args := cmd.Args
	if len(args) < 2 || args[len(args)-2] != "-EncodedCommand" {
		t.Fatalf("args do not use -EncodedCommand: %#v", args)
	}
	if strings.Contains(strings.Join(args, " "), "-Command") {
		t.Fatalf("legacy -Command path still present: %#v", args)
	}
	raw, err := base64.StdEncoding.DecodeString(args[len(args)-1])
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	decoded := string(utf16.Decode(units))
	if decoded != wrapShellCommandForPipe(shellPowerShell, in) {
		t.Fatalf("decoded command mismatch:\n got: %q\nwant: %q", decoded, wrapShellCommandForPipe(shellPowerShell, in))
	}
}

func TestPowerShellSpecialCharactersExecute(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell execution is platform-specific")
	}
	const expected = `quotes 'single' "double" $HOME ` + "`" + `tick 中文 😀`
	cmd, err := buildShellCommand(shellPowerShell, `Write-Output 'quotes ''single'' "double" $HOME `+"`"+`tick 中文 😀'`)
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("PowerShell failed: %v", err)
	}
	if got := strings.TrimSpace(string(bytes.TrimPrefix(output, []byte{0xEF, 0xBB, 0xBF}))); got != expected {
		t.Fatalf("output = %q, want %q", got, expected)
	}
}
