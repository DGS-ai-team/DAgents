package workgroup

import "sync"

// Session 跟踪强身份 WS 连接世代与投递游标（按 workgroup 分离 delivery_seq）。
type Session struct {
	mu                   sync.Mutex
	NodeID               string
	ConnectionGeneration int64
	LastAckDeliverySeq   int64 // 兼容：最近一次 ack（任意组）
	LastAckByWG          map[string]int64
	Active               bool
}

// Hello 建立/替换连接；同 node 重复 hello 会使旧世代失效。
func (s *Session) Hello(nodeID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.NodeID = nodeID
	s.ConnectionGeneration++
	if s.ConnectionGeneration < 1 {
		s.ConnectionGeneration = 1
	}
	s.Active = true
	return s.ConnectionGeneration
}

// OfferResume 返回会话级游标（hello 用；取各组最大 ack）。
func (s *Session) OfferResume() ResumeCursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	max := s.LastAckDeliverySeq
	for _, seq := range s.LastAckByWG {
		if seq > max {
			max = seq
		}
	}
	return ResumeCursor{LastAckDeliverySeq: max}
}

// OfferResumeFor 返回指定工作组的 resume cursor。
func (s *Session) OfferResumeFor(workgroupID string) ResumeCursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.LastAckByWG == nil {
		return ResumeCursor{LastAckDeliverySeq: 0}
	}
	return ResumeCursor{LastAckDeliverySeq: s.LastAckByWG[workgroupID]}
}

// AckDelivery 推进指定工作组的 last_ack_delivery_seq；不允许回退。
func (s *Session) AckDelivery(workgroupID string, seq int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.LastAckByWG == nil {
		s.LastAckByWG = map[string]int64{}
	}
	prev := s.LastAckByWG[workgroupID]
	if seq < prev {
		return errf(CodeConflict, "delivery_seq regress %d < %d for %s", seq, prev, workgroupID)
	}
	s.LastAckByWG[workgroupID] = seq
	s.LastAckDeliverySeq = seq
	return nil
}

// FenceFrame 拒绝旧 connection_generation 帧。
func (s *Session) FenceFrame(connectionGeneration int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Active {
		return errf(CodeNotAuthorized, "session inactive")
	}
	if connectionGeneration != 0 && connectionGeneration != s.ConnectionGeneration {
		return errf(CodeFencingRejected, "stale connection_generation %d != %d", connectionGeneration, s.ConnectionGeneration)
	}
	return nil
}

// Generation 返回当前世代。
func (s *Session) Generation() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ConnectionGeneration
}
