package full

import (
	"fmt"
	"strings"
	"time"
)

const statusTranscriptPrefix = "[status] "

// statusLineManager 管理 prefilling/thinking 等等待态（展示层追加，不写 transcript 存储）。
type statusLineManager struct {
	active map[string]time.Time
}

func newStatusLineManager() *statusLineManager {
	return &statusLineManager{active: make(map[string]time.Time)}
}

func (s *statusLineManager) Reset() {
	s.active = make(map[string]time.Time)
}

func (s *statusLineManager) Start(kind string) {
	if strings.TrimSpace(kind) == "" {
		return
	}
	s.active[kind] = time.Now()
}

func (s *statusLineManager) Finish(kind string) {
	if s == nil || s.active == nil {
		return
	}
	delete(s.active, kind)
}

func (s *statusLineManager) FinishAll() {
	if s == nil {
		return
	}
	s.active = make(map[string]time.Time)
}

func (s *statusLineManager) Has(kind string) bool {
	_, ok := s.active[kind]
	return ok
}

func (s *statusLineManager) FormatLine(kind string) string {
	start, ok := s.active[kind]
	if !ok {
		return ""
	}
	elapsed := int(time.Since(start).Seconds())
	switch kind {
	case "prefilling":
		return statusTranscriptPrefix + fmt.Sprintf("prefilling… %ds", elapsed)
	case "thinking":
		return statusTranscriptPrefix + fmt.Sprintf("thinking… %ds", elapsed)
	case "compression":
		return statusTranscriptPrefix + "上下文压缩中…"
	default:
		return statusTranscriptPrefix + kind
	}
}

func (s *statusLineManager) Kinds() []string {
	out := make([]string, 0, len(s.active))
	for k := range s.active {
		out = append(out, k)
	}
	return out
}
