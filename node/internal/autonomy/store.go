// Package autonomy stores the small, independent state used by the optional
// autonomous/dreaming loop. It deliberately has no dependency on goals.
package autonomy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("autonomy record not found")
var ErrConflict = errors.New("autonomy revision conflict")

type Profile struct {
	AgentID             string `json:"agent_id"`
	Revision            int64  `json:"revision"`
	Responsibility      string `json:"responsibility"`
	WakeIntervalSeconds int64  `json:"wake_interval_seconds"`
	MaxToolRounds       int    `json:"max_tool_rounds"`
	DreamingEnabled     bool   `json:"dreaming_enabled"`
	DreamingTime        string `json:"dreaming_time"`
	Timezone            string `json:"timezone"`
}

type Experience struct {
	AgentID   string    `json:"agent_id"`
	Revision  int64     `json:"revision"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Todo struct {
	ID       string `json:"id"`
	AgentID  string `json:"agent_id"`
	Text     string `json:"text"`
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
}

type disk struct {
	Profiles    map[string]Profile    `json:"profiles"`
	Experiences map[string]Experience `json:"experiences"`
	Todos       map[string]Todo       `json:"todos"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data disk
}

func Open(path string) (*Store, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("autonomy store path required")
	}
	s := &Store{path: path, data: disk{Profiles: map[string]Profile{}, Experiences: map[string]Experience{}, Todos: map[string]Todo{}}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	s.initMaps()
	for id, p := range s.data.Profiles {
		if id != p.AgentID || p.Revision <= 0 {
			return nil, fmt.Errorf("profile identity/revision mismatch")
		}
		if err := validateProfile(p); err != nil {
			return nil, fmt.Errorf("profile %s: %w", id, err)
		}
	}
	for id, e := range s.data.Experiences {
		if id != e.AgentID || e.Revision <= 0 || e.UpdatedAt.IsZero() {
			return nil, fmt.Errorf("experience identity/revision mismatch")
		}
	}
	for id, todo := range s.data.Todos {
		if err := validateTodo(todo); err != nil {
			return nil, fmt.Errorf("todo %s: %w", id, err)
		}
	}
	return s, nil
}

func (s *Store) initMaps() {
	if s.data.Profiles == nil {
		s.data.Profiles = map[string]Profile{}
	}
	if s.data.Experiences == nil {
		s.data.Experiences = map[string]Experience{}
	}
	if s.data.Todos == nil {
		s.data.Todos = map[string]Todo{}
	}
}

func (s *Store) PutProfile(p Profile, expectedRevision int64) error {
	p.AgentID = strings.TrimSpace(p.AgentID)
	if err := validateProfile(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.data.Profiles[p.AgentID]
	if exists && old.Revision != expectedRevision {
		return ErrConflict
	}
	if !exists && expectedRevision != 0 {
		return ErrConflict
	}
	p.Revision = old.Revision + 1
	if err := s.persistLocked(func() { s.data.Profiles[p.AgentID] = p }); err != nil {
		return err
	}
	return nil
}

func (s *Store) GetProfile(agentID string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.data.Profiles[strings.TrimSpace(agentID)]
	return p, ok
}

// GetExperience is the read-only access path for ordinary consumers.
func (s *Store) GetExperience(agentID string) (Experience, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.data.Experiences[strings.TrimSpace(agentID)]
	return e, ok
}

// SubmitDreamingExperience is the explicitly named write path for dreaming.
// Authorization belongs to the runtime boundary that invokes this method.
func (s *Store) SubmitDreamingExperience(agentID string, expectedRevision int64, content string, now time.Time) (Experience, error) {
	agentID, content = strings.TrimSpace(agentID), strings.TrimSpace(content)
	if agentID == "" || expectedRevision < 0 || now.IsZero() {
		return Experience{}, fmt.Errorf("invalid experience")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.data.Experiences[agentID]
	if exists && old.Revision != expectedRevision {
		return Experience{}, ErrConflict
	}
	if !exists && expectedRevision != 0 {
		return Experience{}, ErrConflict
	}
	e := Experience{AgentID: agentID, Revision: old.Revision + 1, Content: content, UpdatedAt: now.UTC()}
	if err := s.persistLocked(func() { s.data.Experiences[agentID] = e }); err != nil {
		return Experience{}, err
	}
	return e, nil
}

func (s *Store) CreateTodo(agentID, text string) (Todo, error) {
	agentID, text = strings.TrimSpace(agentID), strings.TrimSpace(text)
	if agentID == "" || text == "" {
		return Todo{}, fmt.Errorf("invalid todo")
	}
	id, err := newID()
	if err != nil {
		return Todo{}, err
	}
	t := Todo{ID: id, AgentID: agentID, Text: text, Status: "pending", Revision: 1}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.persistLocked(func() { s.data.Todos[id] = t }); err != nil {
		return Todo{}, err
	}
	return t, nil
}

func (s *Store) UpdateTodoCAS(agentID, id string, expectedRevision int64, text, status string) (Todo, error) {
	agentID, id, text, status = strings.TrimSpace(agentID), strings.TrimSpace(id), strings.TrimSpace(text), strings.TrimSpace(status)
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.data.Todos[id]
	if !ok || t.AgentID != agentID {
		return Todo{}, ErrNotFound
	}
	if t.Revision != expectedRevision {
		return Todo{}, ErrConflict
	}
	n := t
	if text != "" {
		n.Text = text
	}
	if status != "" {
		n.Status = status
	}
	n.Revision++
	if err := validateTodo(n); err != nil {
		return Todo{}, err
	}
	if err := s.persistLocked(func() { s.data.Todos[id] = n }); err != nil {
		return Todo{}, err
	}
	return n, nil
}

func (s *Store) DeleteTodoCAS(agentID, id string, expectedRevision int64) error {
	agentID, id = strings.TrimSpace(agentID), strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.data.Todos[id]
	if !ok || t.AgentID != agentID {
		return ErrNotFound
	}
	if t.Revision != expectedRevision {
		return ErrConflict
	}
	return s.persistLocked(func() { delete(s.data.Todos, id) })
}

func (s *Store) GetTodo(agentID, id string) (Todo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.data.Todos[strings.TrimSpace(id)]
	if !ok || t.AgentID != strings.TrimSpace(agentID) {
		return Todo{}, false
	}
	return t, true
}
func (s *Store) ListTodos(agentID string) []Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Todo{}
	for _, t := range s.data.Todos {
		if t.AgentID == strings.TrimSpace(agentID) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func validateProfile(p Profile) error {
	if strings.TrimSpace(p.AgentID) == "" || p.WakeIntervalSeconds < 0 || p.WakeIntervalSeconds > 31536000 || p.MaxToolRounds <= 0 || p.MaxToolRounds > 10000 {
		return fmt.Errorf("invalid profile")
	}
	if p.DreamingEnabled && p.DreamingTime == "" {
		return fmt.Errorf("dreaming time is required")
	}
	if p.DreamingTime != "" {
		if _, err := time.Parse("15:04", p.DreamingTime); err != nil {
			return fmt.Errorf("invalid dreaming time")
		}
	}
	if strings.TrimSpace(p.Timezone) == "" {
		return fmt.Errorf("timezone is required")
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	return nil
}
func validateTodo(t Todo) error {
	if t.AgentID == "" || t.ID == "" || t.Text == "" || t.Revision <= 0 {
		return fmt.Errorf("invalid todo")
	}
	switch t.Status {
	case "pending", "in_progress", "completed":
	default:
		return fmt.Errorf("invalid todo status")
	}
	return nil
}
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func (s *Store) persistLocked(mut func()) error {
	oldBytes, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	restore := func() {
		var old disk
		if json.Unmarshal(oldBytes, &old) == nil {
			s.data = old
			s.initMaps()
		}
	}
	mut()
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		restore()
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		restore()
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		restore()
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		restore()
		return err
	}
	return nil
}
