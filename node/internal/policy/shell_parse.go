package policy

import (
	"runtime"
	"strings"
)

// ShellType 为 bash_run 支持的 shell 类型。
type ShellType string

const (
	ShellBash       ShellType = "bash"
	ShellCmd        ShellType = "cmd"
	ShellPowerShell ShellType = "powershell"
)

// ResolveShellType 解析最终 shell：显式参数优先，否则 Windows→powershell，其余→bash。
func ResolveShellType(raw *string) (ShellType, bool) {
	if raw != nil {
		st := ShellType(strings.ToLower(strings.TrimSpace(*raw)))
		switch st {
		case ShellBash, ShellCmd, ShellPowerShell:
			return st, true
		default:
			return "", false
		}
	}
	if runtime.GOOS == "windows" {
		return ShellPowerShell, true
	}
	return ShellBash, true
}

// ParseCommandRoots 按 shell 语法拆分命令并提取每段首词（root command）。

// 关键边界：任一片段 root 为空时返回 ok=false，调用方应保守要求审批。
func ParseCommandRoots(command string, shellType ShellType) (roots []string, ok bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, false
	}
	var parts []string
	switch shellType {
	case ShellBash:
		parts = splitBashStatements(command)
	case ShellCmd:
		parts = splitCmdStatements(command)
	default:
		parts = splitPowerShellStatements(command)
	}
	if len(parts) == 0 {
		return nil, false
	}
	roots = make([]string, 0, len(parts))
	for _, part := range parts {
		root := extractRootForShell(part, shellType)
		if root == "" {
			return nil, false
		}
		roots = append(roots, root)
	}
	return roots, true
}

func extractRootForShell(commandPart string, shellType ShellType) string {
	tokens, err := splitShellWords(commandPart, shellType == ShellBash)
	if err != nil || len(tokens) == 0 {
		tokens = strings.Fields(strings.TrimSpace(commandPart))
	}
	if len(tokens) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(tokens[0]))
}

func splitShellWords(input string, posix bool) ([]string, error) {
	var tokens []string
	var buf strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	flush := func() {
		if s := strings.TrimSpace(buf.String()); s != "" {
			tokens = append(tokens, s)
		}
		buf.Reset()
	}
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if escaped {
			buf.WriteByte(ch)
			escaped = false
			continue
		}
		if !posix && ch == '^' {
			escaped = true
			buf.WriteByte(ch)
			continue
		}
		if !posix && ch == '`' {
			escaped = true
			buf.WriteByte(ch)
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if !inSingle && !inDouble && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r') {
			flush()
			continue
		}
		buf.WriteByte(ch)
	}
	flush()
	if inSingle || inDouble {
		return nil, fmtShellWordsError()
	}
	return tokens, nil
}

type shellWordsError struct{}

func fmtShellWordsError() error { return shellWordsError{} }

func (shellWordsError) Error() string { return "unclosed quote in shell command" }

// SplitBashStatements 按 bash 语义切分语句（引号外 && || ; | 换行）。
func SplitBashStatements(command string) []string {
	return splitBashStatements(command)
}

func splitBashStatements(command string) []string {
	var parts []string
	var buf strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(command); {
		ch := command[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			buf.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			buf.WriteByte(ch)
			i++
			continue
		}
		if !inSingle && !inDouble {
			if i+1 < len(command) {
				two := command[i : i+2]
				if two == "&&" || two == "||" {
					if part := strings.TrimSpace(buf.String()); part != "" {
						parts = append(parts, part)
					}
					buf.Reset()
					i += 2
					continue
				}
			}
			if ch == ';' || ch == '|' || ch == '\n' {
				if part := strings.TrimSpace(buf.String()); part != "" {
					parts = append(parts, part)
				}
				buf.Reset()
				i++
				continue
			}
		}
		buf.WriteByte(ch)
		i++
	}
	if tail := strings.TrimSpace(buf.String()); tail != "" {
		parts = append(parts, tail)
	}
	return parts
}

func splitCmdStatements(command string) []string {
	return splitDelimitedStatements(command, true)
}

func splitPowerShellStatements(command string) []string {
	return splitDelimitedStatements(command, false)
}

func splitDelimitedStatements(command string, cmdCaretEscape bool) []string {
	var parts []string
	var buf strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); {
		ch := command[i]
		if escaped {
			buf.WriteByte(ch)
			escaped = false
			i++
			continue
		}
		if cmdCaretEscape && ch == '^' {
			escaped = true
			buf.WriteByte(ch)
			i++
			continue
		}
		if !cmdCaretEscape && ch == '`' {
			escaped = true
			buf.WriteByte(ch)
			i++
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			buf.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			buf.WriteByte(ch)
			i++
			continue
		}
		if !inSingle && !inDouble {
			if i+1 < len(command) {
				two := command[i : i+2]
				if two == "&&" || two == "||" {
					if part := strings.TrimSpace(buf.String()); part != "" {
						parts = append(parts, part)
					}
					buf.Reset()
					i += 2
					continue
				}
			}
			delims := ";|\n"
			if cmdCaretEscape {
				delims = "&;|\n"
			}
			if strings.ContainsRune(delims, rune(ch)) {
				if part := strings.TrimSpace(buf.String()); part != "" {
					parts = append(parts, part)
				}
				buf.Reset()
				i++
				continue
			}
		}
		buf.WriteByte(ch)
		i++
	}
	if tail := strings.TrimSpace(buf.String()); tail != "" {
		parts = append(parts, tail)
	}
	return parts
}
