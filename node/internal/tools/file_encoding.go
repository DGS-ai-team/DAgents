package tools

import (
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// fileEncodingToolProperty 为 FS 工具 schema 共用的 encoding 字段。
func fileEncodingToolProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"enum":        []string{"utf-8", "gbk", "gb18030"},
		"description": "可选；磁盘文件字节编码。省略时用 config.yaml tools.file_encoding 或平台默认（Windows 常见 gbk，其它 utf-8）。模型侧 content 始终为 UTF-8。",
	}
}

// defaultFileEncoding 为未配置时的平台默认文件编码。
func defaultFileEncoding() string {
	if runtime.GOOS == "windows" {
		return "gbk"
	}
	return defaultOutputEnc
}

func (r *Registry) resolveFileEncoding(arg *string) string {
	if arg != nil {
		if enc := normalizeOutputEncoding(*arg); enc != "" {
			return enc
		}
	}
	if enc := normalizeOutputEncoding(r.fileEncoding); enc != "" {
		return enc
	}
	return defaultFileEncoding()
}

func decodeFileContent(data []byte, enc string) (string, error) {
	enc = normalizeOutputEncoding(enc)
	if enc == "" {
		enc = defaultFileEncoding()
	}
	switch enc {
	case "utf-8":
		return decodeShellOutput(data, "utf-8"), nil
	case "gbk", "gb18030":
		if text, ok := transcodeShellOutput(data, enc); ok {
			return text, nil
		}
		if text := decodeShellOutput(data, "utf-8"); text != "" {
			return text, nil
		}
		return "", fmt.Errorf("无法用 %s 解码文件内容", enc)
	default:
		return string(data), nil
	}
}

func encodeFileContent(text, enc string) ([]byte, error) {
	enc = normalizeOutputEncoding(enc)
	if enc == "" {
		enc = defaultFileEncoding()
	}
	if enc == "utf-8" {
		return []byte(text), nil
	}
	var t transform.Transformer
	switch enc {
	case "gbk":
		t = simplifiedchinese.GBK.NewEncoder()
	case "gb18030":
		t = simplifiedchinese.GB18030.NewEncoder()
	default:
		return []byte(text), nil
	}
	out, _, err := transform.String(t, text)
	if err != nil {
		return nil, fmt.Errorf("encode file content (%s): %w", enc, err)
	}
	return []byte(out), nil
}

func normalizeLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}
