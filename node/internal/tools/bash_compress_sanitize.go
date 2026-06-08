package tools

import (
	"regexp"
	"strings"
)

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9:;?]*[ -/]*[@-~]|\x1b\][^\x07]*(?:\x07|\x1b\\)`)

// sanitizeCLIOutput 为 L1 通用清洗：ANSI、换行、空行、连续重复行。
func sanitizeCLIOutput(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = ansiEscapeRE.ReplaceAllString(s, "")

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	var prev string
	prevSet := false
	repeatCount := 0

	flushRepeat := func() {
		if repeatCount <= 0 {
			return
		}
		if repeatCount == 1 {
			out = append(out, prev)
		} else {
			out = append(out, prev, repeatLineNote(repeatCount))
		}
		repeatCount = 0
		prevSet = false
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			flushRepeat()
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			continue
		}
		if prevSet && line == prev {
			repeatCount++
			continue
		}
		flushRepeat()
		prev = line
		prevSet = true
		repeatCount = 1
	}
	flushRepeat()

	// 去掉首尾空行
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

func repeatLineNote(count int) string {
	return "[... repeated " + itoa(count) + " identical lines omitted ...]"
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
