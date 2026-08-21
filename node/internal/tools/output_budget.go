package tools

import (
	"bytes"
	"unicode/utf8"
)

// OutputBudget is the byte-level collection boundary for process output.
//
// Providers must apply a budget while bytes are being read, rather than only
// clipping the completed string afterwards. Write reports the input length so
// io.Copy keeps draining the process pipe and a noisy child cannot block on a
// full pipe. The first Limit bytes are retained and the remainder is marked as
// truncated. Higher layers may still apply rune/token/semantic compression.
type OutputBudget struct {
	bytes.Buffer
	limit     int
	tailLimit int
	tail      []byte
	truncated bool
}

// NewOutputBudget creates a head-preserving byte budget. A non-positive limit
// means unlimited collection and is useful for providers that already enforce
// a stronger bound elsewhere.
func NewOutputBudget(limit int) *OutputBudget {
	return &OutputBudget{limit: limit}
}

// NewHeadTailOutputBudget creates a budget that keeps the first headLimit
// bytes and the last tailLimit bytes after the total budget is exceeded. The
// returned value still drains every input byte, so it is safe to attach to a
// process pipe. UTF-8 boundaries are repaired when Bytes is read.
func NewHeadTailOutputBudget(totalLimit, tailLimit int) *OutputBudget {
	if totalLimit <= 0 || tailLimit <= 0 {
		return NewOutputBudget(totalLimit)
	}
	if tailLimit > totalLimit {
		tailLimit = totalLimit
	}
	return &OutputBudget{limit: totalLimit, tailLimit: tailLimit}
}

// Write implements io.Writer while retaining only the configured prefix.
// Returning len(data), nil is intentional: callers should continue draining
// the pipe even after the model-visible budget has been exhausted.
func (b *OutputBudget) Write(data []byte) (int, error) {
	if b == nil {
		return len(data), nil
	}
	originalLen := len(data)
	if b.limit <= 0 {
		return b.Buffer.Write(data)
	}
	if b.tailLimit > 0 {
		headLimit := b.limit - b.tailLimit
		if headLimit < 0 {
			headLimit = 0
		}
		if b.Len() < headLimit {
			remaining := headLimit - b.Len()
			keep := len(data)
			if keep > remaining {
				keep = remaining
			}
			if keep > 0 {
				_, _ = b.Buffer.Write(data[:keep])
				data = data[keep:]
			}
		}
		if len(data) > 0 {
			b.truncated = true
			if len(data) >= b.tailLimit {
				b.tail = append(b.tail[:0], data[len(data)-b.tailLimit:]...)
			} else {
				b.tail = append(b.tail, data...)
				if len(b.tail) > b.tailLimit {
					b.tail = append([]byte(nil), b.tail[len(b.tail)-b.tailLimit:]...)
				}
			}
		}
		return originalLen, nil
	}
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = b.Buffer.Write(data[:remaining])
		b.truncated = true
		return len(data), nil
	}
	return b.Buffer.Write(data)
}

func (b *OutputBudget) Truncated() bool {
	return b != nil && b.truncated
}

// Bytes never exposes a partial UTF-8 sequence when a byte budget ends in the
// middle of a rune. The raw buffer may still keep that byte so a later Write
// can complete the sequence before the process ends.
func (b *OutputBudget) Bytes() []byte {
	if b == nil {
		return nil
	}
	head := safeUTF8Prefix(b.Buffer.Bytes())
	if b.tailLimit <= 0 || !b.truncated {
		return head
	}
	tail := safeUTF8Suffix(b.tail)
	if len(head) == 0 {
		return append([]byte(nil), tail...)
	}
	if len(tail) == 0 {
		return append([]byte(nil), head...)
	}
	const marker = "\n[... output truncated; showing beginning and end ...]\n"
	out := make([]byte, 0, len(head)+len(marker)+len(tail))
	out = append(out, head...)
	out = append(out, marker...)
	out = append(out, tail...)
	return out
}

func safeUTF8Prefix(raw []byte) []byte {
	if utf8.Valid(raw) {
		return raw
	}
	for n := len(raw); n > 0; n-- {
		if utf8.Valid(raw[:n]) {
			return raw[:n]
		}
	}
	return nil
}

func safeUTF8Suffix(raw []byte) []byte {
	if utf8.Valid(raw) {
		return raw
	}
	for n := 0; n < len(raw); n++ {
		if utf8.Valid(raw[n:]) {
			return raw[n:]
		}
	}
	return nil
}

func (b *OutputBudget) String() string {
	return string(b.Bytes())
}
