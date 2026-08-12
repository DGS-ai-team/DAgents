package tools

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

// encodingSource 表示本次选用的文件编码从何而来。
type encodingSource string

const (
	encSourceArgument encodingSource = "参数"
	encSourceCache    encodingSource = "缓存"
	encSourceDetected encodingSource = "检测"
	encSourceConfig   encodingSource = "配置"
)

// pathEncodingChoice 为单条路径一次 FS 操作的编码决策。
type pathEncodingChoice struct {
	Encoding       string
	Source         encodingSource
	Detected       string // 检测最高分编码；与本次相同时为空
	GarbledWarning bool
	UTF8BOM        bool // 写入 utf-8 时带 BOM（原文件已有，或 .ps1/.cmd 自动添加）
}

func stripUTF8BOM(data []byte) ([]byte, bool) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:], true
	}
	return data, false
}

// detectEncodingFromBytes 对磁盘原始字节做启发式编码检测（无外部 chardet 依赖）。
func detectEncodingFromBytes(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	body, hadBOM := stripUTF8BOM(raw)
	if hadBOM {
		return "utf-8"
	}

	bestEnc := ""
	bestScore := -1e9
	record := func(enc string, score float64) {
		if score > bestScore {
			bestScore = score
			bestEnc = enc
		}
	}

	if utf8.Valid(body) {
		record("utf-8", textDecodeScore(string(body))+20)
	}
	if text, ok := transcodeShellOutput(body, "gb18030"); ok {
		record("gb18030", textDecodeScore(text)+5)
	}
	if text, ok := transcodeShellOutput(body, "gbk"); ok {
		record("gbk", textDecodeScore(text))
	}

	if bestEnc == "" || bestScore < 8 {
		return ""
	}
	return bestEnc
}

// textDecodeScore 对解码后的 UTF-8 文本打分，越高越像正常文本。
func textDecodeScore(text string) float64 {
	if text == "" {
		return 0
	}
	var good, bad float64
	for _, r := range text {
		switch {
		case r == '\uFFFD':
			bad += 15
		case unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t':
			bad += 4
		case unicode.IsPrint(r), r == '\n', r == '\r', r == '\t':
			good += 1
		default:
			good += 0.5
		}
	}
	// 惩罚 Mojibake 常见拉丁扩展噪声（UTF-8 误当 gbk 时）
	if bytes.Count([]byte(text), []byte("Ã")) > 2 || bytes.Count([]byte(text), []byte("Â")) > 2 {
		bad += 20
	}
	return good - bad
}

func textLooksGarbled(text string) bool {
	if text == "" {
		return false
	}
	score := textDecodeScore(text)
	runes := utf8.RuneCountInString(text)
	if runes < 24 {
		return score/float64(runes) < 0.75
	}
	return score < 12
}

func decodePathFileContent(raw []byte, enc string) (string, error) {
	enc = normalizeOutputEncoding(enc)
	if enc == "" {
		enc = defaultFileEncoding()
	}
	body, _ := stripUTF8BOM(raw)
	switch enc {
	case "utf-8":
		if !utf8.Valid(body) {
			return "", errEncodingDecode("utf-8", "字节序列不是合法 UTF-8")
		}
		return string(body), nil
	case "gbk", "gb18030":
		if text, ok := transcodeShellOutput(body, enc); ok {
			return text, nil
		}
		return "", errEncodingDecode(enc, "无法用该编码解码文件内容")
	default:
		if utf8.Valid(body) {
			return string(body), nil
		}
		return string(body), nil
	}
}

func errEncodingDecode(enc, msg string) error {
	return fmtErrorEncoding(enc, msg)
}

// fmtErrorEncoding 供 FS 工具返回用户可见错误行。
func fmtErrorEncoding(enc, msg string) error {
	return &encodingDecodeError{enc: enc, msg: msg}
}

type encodingDecodeError struct {
	enc string
	msg string
}

func (e *encodingDecodeError) Error() string {
	return e.msg + "（encoding=" + e.enc + "）"
}
