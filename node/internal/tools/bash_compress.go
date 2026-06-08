package tools

import (
	"strings"
	"unicode/utf8"
)

type bashStreamCompressMeta struct {
	enabled       bool
	inRunes       int
	outRunes      int
	sanitized     bool
	runeTruncated bool
}

func compressBashStream(cfg BashCompressConfig, text string, maxRunes int) (string, bashStreamCompressMeta) {
	inRunes := utf8.RuneCountInString(text)
	meta := bashStreamCompressMeta{
		enabled: cfg.Enabled,
		inRunes: inRunes,
	}
	if text == "" {
		return text, meta
	}

	out := strings.TrimSpace(text)
	if cfg.Enabled {
		before := out
		out = sanitizeCLIOutput(out)
		meta.sanitized = out != before
	}

	out, meta.runeTruncated = clipTextRunes(out, maxRunes)
	meta.outRunes = utf8.RuneCountInString(out)
	return out, meta
}

func stderrMaxRunes(cfg BashCompressConfig, _ int) int {
	if cfg.MaxOutputCharsStderr > 0 {
		return cfg.MaxOutputCharsStderr
	}
	return cfg.MaxOutputChars
}
