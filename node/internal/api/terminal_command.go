package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

const (
	terminalCommandDefaultTimeout = 120 * time.Second
	terminalCommandInterruptGrace = 1500 * time.Millisecond
	terminalCommandReplayBytes    = 1 << 20
)

// runCommand serializes command wrappers per terminal, while still allowing
// terminal_input to write concurrently so an interactive prompt can be
// answered. The wrapper is sent through the same PTY as terminal_input and
// uses an unguessable marker to identify the command's actual completion.
func (s *terminalSession) runCommand(ctx context.Context, req tools.TerminalCommandRequest) (tools.TerminalCommandResult, error) {
	if s == nil || s.terminal == nil {
		return tools.TerminalCommandResult{}, fmt.Errorf("terminal session is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.commandMu.Lock()
	defer s.commandMu.Unlock()

	s.mu.Lock()
	closed := s.closed
	exited := s.exited != nil
	targetKind := s.targetKind
	shell := s.shell
	s.mu.Unlock()
	if closed {
		return tools.TerminalCommandResult{}, fmt.Errorf("terminal session is closed")
	}
	if exited {
		return tools.TerminalCommandResult{}, fmt.Errorf("terminal session has exited")
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = terminalCommandDefaultTimeout
	}
	maxOutput := req.MaxOutputBytes
	if maxOutput <= 0 || maxOutput > terminalCommandReplayBytes {
		maxOutput = terminalCommandReplayBytes
	}
	token, err := newTerminalCommandToken()
	if err != nil {
		return tools.TerminalCommandResult{}, err
	}
	input, startMarker, endPrefix, err := buildTerminalCommandInput(shell, req.Command, req.CWD, token)
	if err != nil {
		return tools.TerminalCommandResult{}, err
	}
	baseline := s.currentOutputSeq()
	if err := s.input(ctx, input); err != nil {
		return tools.TerminalCommandResult{}, err
	}

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	var interruptTimer *time.Timer
	var interruptC <-chan time.Time
	ctxDone := ctx.Done()
	interrupted := false
	cancelled := false
	timedOut := false
	interruptErr := ""

	interrupt := func() {
		if interrupted {
			return
		}
		interrupted = true
		interruptTimer = time.NewTimer(terminalCommandInterruptGrace)
		interruptC = interruptTimer.C
		interruptCtx, cancel := context.WithTimeout(context.Background(), terminalForceTimeout)
		err := s.input(interruptCtx, []byte{0x03})
		cancel()
		if err != nil {
			interruptErr = err.Error()
		}
	}

	for {
		transcript, nextSeq, terminalExited, wake := s.commandSnapshot(baseline)
		output, code, done, started := parseTerminalCommandTranscript(transcript, startMarker, endPrefix)
		if done {
			s.consumeOutputThrough(nextSeq)
			result := makeTerminalCommandResult(req, targetKind, output, code, maxOutput, cancelled, timedOut)
			if interruptErr != "" && result.Error == "" {
				result.Error = "interrupt terminal input failed: " + interruptErr
			}
			return result, nil
		}
		if terminalExited {
			s.consumeOutputThrough(nextSeq)
			result := makeTerminalCommandResult(req, targetKind, output, 1, maxOutput, cancelled, timedOut)
			if !started {
				result.Error = "terminal exited before command started"
			} else {
				result.Error = "terminal exited before command completed"
			}
			if interruptErr != "" {
				result.Error += ": " + interruptErr
			}
			return result, nil
		}
		if interrupted {
			select {
			case <-interruptC:
				s.consumeOutputThrough(nextSeq)
				result := makeTerminalCommandResult(req, targetKind, output, 130, maxOutput, cancelled, timedOut)
				result.Error = "terminal command interrupted before completion"
				if interruptErr != "" {
					result.Error += ": " + interruptErr
				}
				return result, nil
			case <-wake:
				continue
			}
		}
		select {
		case <-ctxDone:
			ctxDone = nil
			cancelled = true
			interrupt()
		case <-timeoutTimer.C:
			timedOut = true
			interrupt()
		case <-wake:
		}
	}
}

func (s *terminalSession) currentOutputSeq() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextSeq
}

func (s *terminalSession) commandSnapshot(afterSeq uint64) ([]byte, uint64, bool, <-chan struct{}) {
	out := s.snapshotOutput(afterSeq, terminalCommandReplayBytes, false)
	var data bytes.Buffer
	for _, chunk := range out.Chunks {
		_, _ = data.Write(chunk.Data)
	}
	s.mu.Lock()
	currentSeq := s.nextSeq
	wake := s.outputWake
	exited := s.exited != nil
	s.mu.Unlock()
	if currentSeq > out.NextSeq {
		// A frame arrived between snapshotOutput and the state read. The
		// caller will either observe the already-advanced sequence or wake on
		// this channel; returning the current sequence avoids a missed wake.
		out.NextSeq = currentSeq
	}
	return data.Bytes(), out.NextSeq, exited, wake
}

func (s *terminalSession) consumeOutputThrough(seq uint64) {
	s.mu.Lock()
	if seq > s.toolSeq {
		s.toolSeq = seq
	}
	s.mu.Unlock()
}

func newTerminalCommandToken() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create terminal command marker: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func buildTerminalCommandInput(shell, command, cwd, token string) ([]byte, string, string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, "", "", fmt.Errorf("command is required")
	}
	if strings.ContainsRune(command, '\x00') || strings.ContainsRune(cwd, '\x00') {
		return nil, "", "", fmt.Errorf("terminal command contains NUL")
	}
	start := "__DAGENTS_COMMAND_START_" + token + "__"
	endPrefix := "__DAGENTS_COMMAND_END_" + token + "_"
	switch normalized := strings.ToLower(strings.TrimSpace(shell)); normalized {
	case "", "bash", "sh", "zsh", "wsl":
		return buildPOSIXTerminalCommandInput(command, cwd, start, endPrefix)
	case "powershell", "pwsh":
		return buildPowerShellTerminalCommandInput(command, cwd, start, endPrefix)
	case "cmd":
		return buildCMDTerminalCommandInput(command, cwd, start, endPrefix)
	default:
		if runtime.GOOS == "windows" {
			return nil, "", "", fmt.Errorf("terminal command does not support shell %q", shell)
		}
		return buildPOSIXTerminalCommandInput(command, cwd, start, endPrefix)
	}
}

func buildPOSIXTerminalCommandInput(command, cwd, start, endPrefix string) ([]byte, string, string, error) {
	var b strings.Builder
	// A persistent PTY echoes submitted lines. Disable echo before the
	// completion envelope so the command result contains command output rather
	// than the wrapper's prompts/typed lines; restore it after the marker.
	b.WriteString("stty -echo 2>/dev/null; __DAGENTS_OLD_PS1=$PS1; __DAGENTS_OLD_PS2=$PS2; PS1=''; PS2=''\nprintf '\\n")
	b.WriteString(start)
	b.WriteString("\\n'\n(\n  set +e\n  __DAGENTS_COMMAND=")
	b.WriteString(posixShellQuote(command))
	b.WriteString("\n  __DAGENTS_RC=0\n")
	if strings.TrimSpace(cwd) != "" {
		fmt.Fprintf(&b, "  cd -- %s\n  __DAGENTS_RC=$?\n", posixShellQuote(cwd))
	}
	b.WriteString("  if [ \"$__DAGENTS_RC\" -eq 0 ]; then\n")
	b.WriteString("    eval \"$__DAGENTS_COMMAND\"\n    __DAGENTS_RC=$?\n  fi\n")
	fmt.Fprintf(&b, "  printf '\\n%s%%s__\\n' \"$__DAGENTS_RC\"\n)\nPS1=\"$__DAGENTS_OLD_PS1\"; PS2=\"$__DAGENTS_OLD_PS2\"; stty echo 2>/dev/null\n", endPrefix)
	return []byte(b.String()), start, endPrefix, nil
}

func buildPowerShellTerminalCommandInput(command, cwd, start, endPrefix string) ([]byte, string, string, error) {
	var b strings.Builder
	// ConPTY feeds this payload into an interactive PowerShell prompt. A bare
	// LF is treated as line-feed input by the Windows console and leaves the
	// parser in the continuation prompt (>>); a real Enter is CRLF. Keep every
	// generated line CRLF, just like the terminal UI's Enter key.
	const lineEnd = "\r\n"
	writeLine := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
		b.WriteString(lineEnd)
	}
	writeLine("Write-Output \"\"")
	writeLine("Write-Output %s", powerShellQuote(start))
	writeLine("& {")
	writeLine("  $__DAGENTS_COMMAND = %s", powerShellQuote(command))
	writeLine("  $__DAGENTS_RC = 0")
	writeLine("  $LASTEXITCODE = 0")
	if strings.TrimSpace(cwd) != "" {
		writeLine("  Push-Location -LiteralPath %s", powerShellQuote(cwd))
		writeLine("  if (-not $?) { $__DAGENTS_RC = 1 }")
	}
	writeLine("  if ($__DAGENTS_RC -eq 0) {")
	writeLine("    try {")
	writeLine("      Invoke-Expression $__DAGENTS_COMMAND")
	writeLine("      if ($LASTEXITCODE -ne 0) { $__DAGENTS_RC = [int]$LASTEXITCODE } elseif ($?) { $__DAGENTS_RC = 0 } else { $__DAGENTS_RC = 1 }")
	writeLine("    } catch {")
	writeLine("      Write-Error $_")
	writeLine("      $__DAGENTS_RC = 1")
	writeLine("    }")
	writeLine("  }")
	if strings.TrimSpace(cwd) != "" {
		writeLine("  Pop-Location")
	}
	writeLine("  Write-Output ('%s' + $__DAGENTS_RC + '__')", endPrefix)
	writeLine("}")
	return []byte(b.String()), start, endPrefix, nil
}

func buildCMDTerminalCommandInput(command, cwd, start, endPrefix string) ([]byte, string, string, error) {
	var b strings.Builder
	b.WriteString("@echo off\r\nsetlocal EnableExtensions DisableDelayedExpansion\r\necho.\r\necho ")
	b.WriteString(start)
	b.WriteString("\r\n")
	if strings.TrimSpace(cwd) != "" {
		fmt.Fprintf(&b, "pushd \"%s\"\r\n", strings.ReplaceAll(cwd, "\"", "\"\""))
	}
	b.WriteString(command)
	b.WriteString("\r\nset \"__DAGENTS_RC=%ERRORLEVEL%\"\r\n")
	if strings.TrimSpace(cwd) != "" {
		b.WriteString("popd\r\n")
	}
	fmt.Fprintf(&b, "echo %s%%__DAGENTS_RC%%\r\nendlocal\r\n", endPrefix)
	return []byte(b.String()), start, endPrefix, nil
}

func posixShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func parseTerminalCommandTranscript(data []byte, startMarker, endPrefix string) ([]byte, int, bool, bool) {
	startEnd := -1
	for pos := 0; pos < len(data); {
		lineEnd := bytes.IndexByte(data[pos:], '\n')
		if lineEnd < 0 {
			break
		}
		lineEnd += pos
		line := strings.TrimSuffix(string(data[pos:lineEnd]), "\r")
		// Interactive PowerShell may echo the submitted line with a prompt
		// prefix (for example ">> ") or terminal control bytes. The marker is
		// still authoritative because it contains a random per-call token.
		if strings.Contains(line, startMarker) {
			startEnd = lineEnd + 1
			break
		}
		pos = lineEnd + 1
	}
	if startEnd < 0 {
		return nil, 1, false, false
	}
	for pos := startEnd; pos < len(data); {
		lineEnd := bytes.IndexByte(data[pos:], '\n')
		if lineEnd < 0 {
			break
		}
		lineEnd += pos
		line := strings.TrimSuffix(string(data[pos:lineEnd]), "\r")
		if markerAt := strings.Index(line, endPrefix); markerAt >= 0 {
			rawCode := line[markerAt+len(endPrefix):]
			if suffixAt := strings.Index(rawCode, "__"); suffixAt >= 0 {
				code, err := strconv.Atoi(strings.TrimSpace(rawCode[:suffixAt]))
				if err == nil {
					output := data[startEnd:pos]
					output = trimOneLineEnding(output)
					return cleanTerminalCommandOutput(output), code, true, true
				}
			}
		}
		pos = lineEnd + 1
	}
	return data[startEnd:], 1, false, true
}

// cleanTerminalCommandOutput removes terminal-driver prompt/control lines
// that can be emitted by bracketed-paste mode while a multiline wrapper is
// being entered. It intentionally runs after marker extraction, so it cannot
// hide the completion marker or affect the persistent terminal transcript.
func cleanTerminalCommandOutput(data []byte) []byte {
	lines := bytes.Split(data, []byte{'\n'})
	cleaned := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if bytes.Contains(line, []byte("\x1b[?2004h")) || bytes.Contains(line, []byte("\x1b[?2004l")) {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return bytes.Join(cleaned, []byte{'\n'})
}

func trimOneLineEnding(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
		if len(data) > 0 && data[len(data)-1] == '\r' {
			data = data[:len(data)-1]
		}
	}
	return data
}

func makeTerminalCommandResult(req tools.TerminalCommandRequest, targetKind string, output []byte, code, maxOutput int, cancelled, timedOut bool) tools.TerminalCommandResult {
	truncated := len(output) > maxOutput
	if truncated {
		output = output[:maxOutput]
	}
	status := "SUCCEEDED"
	if cancelled {
		status = "CANCELLED"
	} else if timedOut || code != 0 {
		status = "FAILED"
	}
	result := tools.TerminalCommandResult{
		Status: status, TerminalID: req.TerminalID, TargetKind: targetKind, ExitCode: code,
		Stdout: string(output), StdoutBytes: len(output), OutputTruncated: truncated,
		Cancelled: cancelled, TimedOut: timedOut,
	}
	if cancelled {
		result.Error = "terminal command cancelled"
	} else if timedOut {
		result.Error = "terminal command timed out"
	} else if code != 0 {
		result.Error = fmt.Sprintf("terminal command exited with code %d", code)
	}
	return result
}
