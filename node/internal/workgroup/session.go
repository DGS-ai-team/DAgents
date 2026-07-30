package workgroup

import "sync"

// Session 跟踪强身份 WS 连接世代与投递游标（D2 内存骨架）。
type Session struct {
	mu                   sync.Mutex
	NodeID               string
	ConnectionGeneration int64
	LastAckDeliverySeq   int64
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

// OfferResume 返回当前 resume cursor。
func (s *Session) OfferResume() ResumeCursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ResumeCursor{LastAckDeliverySeq: s.LastAckDeliverySeq}
}

// AckDelivery 推进 last_ack_delivery_seq；不允许回退。
func (s *Session) AckDelivery(seq int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq < s.LastAckDeliverySeq {
		return errf(CodeConflict, "delivery_seq regress %d < %d", seq, s.LastAckDeliverySeq)
	}
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
