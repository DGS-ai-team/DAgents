package full

import (
	"fmt"
	"strings"
	"time"
)

const statusTranscriptPrefix = "[status] "

var statusKindOrder = []string{"prefilling", "thinking", "compression"}

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

func statusKindLabel(kind string) string {
	switch kind {
	case "prefilling":
		return "准备上下文"
	case "thinking":
		return "思考中"
	case "compression":
		return "压缩上下文"
	default:
		return kind
	}
}

func statusAnimatedDots(elapsedSec int) string {
	frame := (elapsedSec * 2) % 3
	return strings.Repeat(".", frame+1) + strings.Repeat(" ", 2-frame)
}

func (s *statusLineManager) FormatLine(kind string) string {
	start, ok := s.active[kind]
	if !ok {
		return ""
	}
	elapsed := int(time.Since(start).Seconds())
	label := statusKindLabel(kind)
	dots := statusAnimatedDots(elapsed)
	return statusTranscriptPrefix + fmt.Sprintf("%s%s %ds", label, dots, elapsed)
}

// ActivePhaseLabel 返回当前最高优先级等待态的中文标签（供顶栏展示）。
func (s *statusLineManager) ActivePhaseLabel() string {
	if s == nil || s.active == nil {
		return ""
	}
	for _, kind := range statusKindOrder {
		if s.Has(kind) {
			return statusKindLabel(kind)
		}
	}
	return ""
}

func (s *statusLineManager) Kinds() []string {
	if s == nil || len(s.active) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.active))
	for _, kind := range statusKindOrder {
		if s.Has(kind) {
			out = append(out, kind)
		}
	}
	for k := range s.active {
		found := false
		for _, o := range statusKindOrder {
			if k == o {
				found = true
				break
			}
		}
		if !found {
			out = append(out, k)
		}
	}
	return out
}
