package workgroup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CommandJournal 持久化 tool command 状态；accept 必须先 journal 再 ack。
type CommandJournal interface {
	Get(commandID string) (*JournalEntry, error)
	Put(entry JournalEntry) error
}

// MemoryJournal 进程内 journal。
type MemoryJournal struct {
	mu   sync.Mutex
	byID map[string]JournalEntry
}

func NewMemoryJournal() *MemoryJournal {
	return &MemoryJournal{byID: make(map[string]JournalEntry)}
}

func (j *MemoryJournal) Get(commandID string) (*JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	e, ok := j.byID[commandID]
	if !ok {
		return nil, nil
	}
	cp := e
	return &cp, nil
}

func (j *MemoryJournal) Put(entry JournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.byID[entry.CommandID] = entry
	return nil
}

// DirJournal 将条目落在 dir/<command_id>.json。
type DirJournal struct {
	dir string
	mu  sync.Mutex
}

func NewDirJournal(dir string) (*DirJournal, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DirJournal{dir: dir}, nil
}

func (j *DirJournal) path(commandID string) string {
	return filepath.Join(j.dir, commandID+".json")
}

func (j *DirJournal) Get(commandID string) (*JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	raw, err := os.ReadFile(j.path(commandID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var e JournalEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (j *DirJournal) Put(entry JournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	if entry.JournaledAt == "" {
		entry.JournaledAt = now
	}
	entry.UpdatedAt = now
	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(j.path(entry.CommandID), raw, 0o644)
}
