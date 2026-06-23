package tools

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// fileEncodingToolProperty 为 FS 工具 schema 共用的 encoding 字段。
func fileEncodingToolProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"enum":        []string{"utf-8", "gbk", "gb18030"},
		"description": "可选；磁盘文件字节编码。省略时用 config.yaml tools.file_encoding 或默认 utf-8。模型侧 content 始终为 UTF-8。",
	}
}

// defaultFileEncoding 为未配置时的默认文件编码（全平台 utf-8）。
func defaultFileEncoding() string {
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
	return encodeFileContentWithBOM(text, enc, false)
}

// encodeFileContentWithBOM 编码文本；utf8BOM 为 true 且 enc=utf-8 时在字节前写入 EF BB BF。
func encodeFileContentWithBOM(text, enc string, utf8BOM bool) ([]byte, error) {
	out, err := encodeFileContentRaw(text, enc)
	if err != nil {
		return nil, err
	}
	if utf8BOM && normalizeOutputEncoding(enc) == "utf-8" {
		return append(append([]byte(nil), utf8BOMPrefix...), out...), nil
	}
	return out, nil
}

var utf8BOMPrefix = []byte{0xEF, 0xBB, 0xBF}

func encodeFileContentRaw(text, enc string) ([]byte, error) {
	enc = normalizeOutputEncoding(enc)
	if enc == "" {
		enc = defaultFileEncoding()
	}
	if enc == "utf-8" {
		return []byte(text), nil
	}
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "?")
	}
	out, err := encodeTextToLegacyChinese(text, enc)
	if err != nil {
		return nil, fmt.Errorf("encode file content (%s): %w", enc, err)
	}
	return out, nil
}

// encodeTextToLegacyChinese 将 UTF-8 文本编码为 GBK/GB18030 字节。
//
// 逻辑：
// 1. gbk：先 GBK；失败则用 GB18030（超集，覆盖 GBK 未收录的 Unicode）；
// 2. gb18030：直接 GB18030；
// 3. 仍失败时按 rune 编码，不可表示字符替换为 ASCII `?`。
func encodeTextToLegacyChinese(text, enc string) ([]byte, error) {
	switch enc {
	case "gbk":
		if out, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(text)); err == nil {
			return out, nil
		}
		if out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(text)); err == nil {
			return out, nil
		}
		return encodeTextWithReplacement(text, simplifiedchinese.GB18030.NewEncoder())
	case "gb18030":
		if out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(text)); err == nil {
			return out, nil
		}
		return encodeTextWithReplacement(text, simplifiedchinese.GB18030.NewEncoder())
	default:
		return []byte(text), nil
	}
}

func encodeTextWithReplacement(text string, enc transform.Transformer) ([]byte, error) {
	var buf bytes.Buffer
	for _, r := range text {
		if r < utf8.RuneSelf {
			buf.WriteByte(byte(r))
			continue
		}
		out, _, err := transform.Bytes(enc, []byte(string(r)))
		if err != nil {
			buf.WriteByte('?')
			continue
		}
		buf.Write(out)
	}
	return buf.Bytes(), nil
}

func normalizeLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}
