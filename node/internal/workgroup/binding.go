package workgroup

import (
	"sync"
)

// BindingStore 持久化 WorkerBinding（当前生产用内存表；测试可用 Memory）。
type BindingStore interface {
	Get(memberID string) (*WorkerBinding, error)
	GetByProvisionID(provisionID string) (*WorkerBinding, error)
	Put(b WorkerBinding) error
	List() ([]WorkerBinding, error)
}

// MemoryBindingStore 进程内绑定表。
type MemoryBindingStore struct {
	mu          sync.Mutex
	byMember    map[string]WorkerBinding
	byProvision map[string]string // provision_id -> member_id
}

func NewMemoryBindingStore() *MemoryBindingStore {
	return &MemoryBindingStore{
		byMember:    make(map[string]WorkerBinding),
		byProvision: make(map[string]string),
	}
}

func (s *MemoryBindingStore) Get(memberID string) (*WorkerBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.byMember[memberID]
	if !ok {
		return nil, nil
	}
	cp := b
	return &cp, nil
}

func (s *MemoryBindingStore) GetByProvisionID(provisionID string) (*WorkerBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mid, ok := s.byProvision[provisionID]
	if !ok {
		return nil, nil
	}
	b, ok := s.byMember[mid]
	if !ok {
		return nil, nil
	}
	cp := b
	return &cp, nil
}

func (s *MemoryBindingStore) Put(b WorkerBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.byMember[b.MemberID]; ok && old.ProvisionID != "" && old.ProvisionID != b.ProvisionID {
		delete(s.byProvision, old.ProvisionID)
	}
	s.byMember[b.MemberID] = b
	if b.ProvisionID != "" {
		s.byProvision[b.ProvisionID] = b.MemberID
	}
	return nil
}

func (s *MemoryBindingStore) List() ([]WorkerBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WorkerBinding, 0, len(s.byMember))
	for _, b := range s.byMember {
		out = append(out, b)
	}
	return out, nil
}
