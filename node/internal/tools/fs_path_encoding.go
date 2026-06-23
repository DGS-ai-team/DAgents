package tools

import (
	"os"
	"strings"
	"time"
)

type pathEncodingEntry struct {
	Encoding string
	Mtime    time.Time
	Source   encodingSource
}

// choosePathEncoding 决定本次 FS 操作使用的磁盘编码（见 §3.2.2 阶段 2）。
func (r *Registry) choosePathEncoding(relPath string, raw []byte, mtime time.Time, argEnc *string) pathEncodingChoice {
	key := cachePathKey(relPath)
	if argEnc != nil {
		if enc := normalizeOutputEncoding(*argEnc); enc != "" {
			return pathEncodingChoice{Encoding: enc, Source: encSourceArgument}
		}
	}
	if r != nil {
		if ent, ok := r.lookupPathEncoding(key, mtime); ok {
			return pathEncodingChoice{Encoding: ent.Encoding, Source: encSourceCache}
		}
	}
	detected := detectEncodingFromBytes(raw)
	configEnc := ""
	if r != nil {
		configEnc = r.resolveFileEncoding(nil)
	} else {
		configEnc = defaultFileEncoding()
	}
	if detected != "" {
		enc := alignDetectedEncoding(detected, configEnc)
		out := pathEncodingChoice{Encoding: enc, Source: encSourceDetected}
		if configEnc != "" && configEnc != enc && configEnc != detected {
			out.Detected = detected
		}
		return out
	}
	return pathEncodingChoice{Encoding: configEnc, Source: encSourceConfig}
}

func (r *Registry) rememberPathEncoding(relPath, encoding string, mtime time.Time, source encodingSource) {
	if r == nil || strings.TrimSpace(encoding) == "" {
		return
	}
	key := cachePathKey(relPath)
	r.pathEncMu.Lock()
	defer r.pathEncMu.Unlock()
	if r.pathEncCache == nil {
		r.pathEncCache = make(map[string]pathEncodingEntry)
	}
	r.pathEncCache[key] = pathEncodingEntry{
		Encoding: normalizeOutputEncoding(encoding),
		Mtime:    mtime,
		Source:   source,
	}
}

func (r *Registry) lookupPathEncoding(key string, mtime time.Time) (pathEncodingEntry, bool) {
	r.pathEncMu.Lock()
	defer r.pathEncMu.Unlock()
	if r.pathEncCache == nil {
		return pathEncodingEntry{}, false
	}
	ent, ok := r.pathEncCache[key]
	if !ok || !mtime.Equal(ent.Mtime) {
		return pathEncodingEntry{}, false
	}
	return ent, true
}

func cachePathKey(relPath string) string {
	return strings.TrimSpace(strings.ReplaceAll(relPath, "\\", "/"))
}

// alignDetectedEncoding 将检测到的 gb 族编码与配置默认对齐，避免 gb18030/gbk 标签抖动。
func alignDetectedEncoding(detected, configEnc string) string {
	switch {
	case configEnc == "gbk" && detected == "gb18030":
		return "gbk"
	case configEnc == "gb18030" && detected == "gbk":
		return "gb18030"
	default:
		return detected
	}
}

func formatEncodingHeaderLines(choice pathEncodingChoice, garbled bool) []string {
	lines := []string{
		"文件编码: " + choice.Encoding,
		"编码来源: " + string(choice.Source),
	}
	if choice.Detected != "" && choice.Source != encSourceDetected {
		lines = append(lines, "检测参考编码: "+choice.Detected)
	}
	if garbled {
		lines = append(lines, "编码提示: 正文疑似乱码，可尝试 encoding=utf-8 或 encoding=gbk 重新读取")
	}
	if choice.UTF8BOM {
		lines = append(lines, "UTF-8 BOM: 是（search_replace/write_file 写入时将保留）")
	}
	return lines
}

// readRawFile 读取路径下全部字节与 mtime。
func readRawFile(absPath string) ([]byte, time.Time, error) {
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, time.Time{}, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return raw, time.Time{}, nil
	}
	return raw, info.ModTime(), nil
}

// readTextLinesAt 按路径编码决策读取文本行，并更新编码缓存。
func (r *Registry) readTextLinesAt(relPath, absPath string, argEnc *string) ([]string, pathEncodingChoice, error) {
	raw, mtime, err := readRawFile(absPath)
	if err != nil {
		return nil, pathEncodingChoice{}, err
	}
	choice := r.choosePathEncoding(relPath, raw, mtime, argEnc)
	choice.UTF8BOM = fileHadUTF8BOM(raw, choice.Encoding)
	text, err := decodePathFileContent(raw, choice.Encoding)
	if err != nil {
		return nil, choice, err
	}
	choice.GarbledWarning = textLooksGarbled(text)
	r.rememberPathEncoding(relPath, choice.Encoding, mtime, choice.Source)
	return normalizeLines(text), choice, nil
}

func fileHadUTF8BOM(raw []byte, enc string) bool {
	if normalizeOutputEncoding(enc) != "utf-8" {
		return false
	}
	_, had := stripUTF8BOM(raw)
	return had
}

func (r *Registry) resolveWriteEncodingChoice(relPath, absPath string, argEnc *string) (pathEncodingChoice, error) {
	raw := []byte{}
	mtime := time.Time{}
	if info, err := os.Stat(absPath); err == nil && !info.IsDir() {
		var errRead error
		raw, mtime, errRead = readRawFile(absPath)
		if errRead != nil {
			return pathEncodingChoice{}, errRead
		}
	}
	choice := r.choosePathEncoding(relPath, raw, mtime, argEnc)
	choice.UTF8BOM = fileHadUTF8BOM(raw, choice.Encoding)
	return choice, nil
}
