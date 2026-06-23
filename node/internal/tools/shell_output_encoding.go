package tools

import (
	"runtime"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// resolveShellOutputEncoding 决定子进程 stdout/stderr 的字节编码，解码为 UTF-8 后交给 LLM。
//
// 优先级：bash_run output_encoding > config.yaml tools.bash_output_encoding > 默认 utf-8。
func resolveShellOutputEncoding(st shellType, configured string) string {
	if enc := normalizeOutputEncoding(configured); enc != "" {
		return enc
	}
	_ = st
	return defaultOutputEnc
}

func normalizeOutputEncoding(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "utf8", "utf-8":
		return "utf-8"
	case "gbk", "cp936", "gb2312":
		return "gbk"
	case "gb18030":
		return "gb18030"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func decodeShellOutput(data []byte, enc string) string {
	if len(data) == 0 {
		return ""
	}
	enc = normalizeOutputEncoding(enc)
	if enc == "" {
		enc = defaultOutputEnc
	}
	switch enc {
	case "utf-8":
		if utf8.Valid(data) {
			return string(data)
		}
		if runtime.GOOS == "windows" {
			if text, ok := transcodeShellOutput(data, "gbk"); ok {
				return text
			}
		}
		return string(data)
	case "gbk", "gb18030":
		// UTF-8 工具（agent-browser 等）在全局 gbk 配置下仍可能输出合法 UTF-8 字节。
		if utf8.Valid(data) {
			return string(data)
		}
		if text, ok := transcodeShellOutput(data, enc); ok {
			return text
		}
		return string(data)
	default:
		if utf8.Valid(data) {
			return string(data)
		}
		return string(data)
	}
}

func transcodeShellOutput(data []byte, enc string) (string, bool) {
	var t transform.Transformer
	switch enc {
	case "gbk":
		t = simplifiedchinese.GBK.NewDecoder()
	case "gb18030":
		t = simplifiedchinese.GB18030.NewDecoder()
	default:
		return "", false
	}
	out, _, err := transform.Bytes(t, data)
	if err != nil || len(out) == 0 {
		return "", false
	}
	if !utf8.Valid(out) {
		return "", false
	}
	return string(out), true
}
