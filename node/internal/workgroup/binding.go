package workgroup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// BindingStore 持久化 WorkerBinding（生产用 DirBindingStore；测试可用 Memory）。
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

// DirBindingStore 将绑定落在 dir/<member_id>.json（重启后可继续执行 tool.command）。
type DirBindingStore struct {
	dir string
	mu  sync.Mutex
}

func NewDirBindingStore(dir string) (*DirBindingStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DirBindingStore{dir: dir}, nil
}

func (s *DirBindingStore) path(memberID string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, strings.TrimSpace(memberID))
	if safe == "" {
		safe = "_unknown"
	}
	return filepath.Join(s.dir, safe+".json")
}

func (s *DirBindingStore) Get(memberID string) (*WorkerBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path(memberID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b WorkerBinding
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *DirBindingStore) GetByProvisionID(provisionID string) (*WorkerBinding, error) {
	pid := strings.TrimSpace(provisionID)
	if pid == "" {
		return nil, nil
	}
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ProvisionID == pid {
			cp := list[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *DirBindingStore) Put(b WorkerBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(b.MemberID), raw, 0o644)
}

func (s *DirBindingStore) List() ([]WorkerBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]WorkerBinding, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var b WorkerBinding
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}
