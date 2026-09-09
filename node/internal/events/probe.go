// Package events provides bounded, read-only event probes for Auto schedules.
package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrOwnerConflict = errors.New("event probe owner/config conflict")
var ErrRetryBackoff = errors.New("event probe retry backoff")

type Config struct {
	SourceID, OwnerAgentID string
	Revision               int64
	Root                   string
	MaxFiles, MaxBytes     int
	HashContent            bool
	Timeout                time.Duration
	IntentID               string
	Generation             int64
	EventFilter            map[string]any
}

// SourceRegistration is the durable, owner-scoped declaration consumed by a
// probe coordinator. Only the built-in read-only directory provider is
// represented here; no command, URL, or executable filter is accepted.
type SourceRegistration struct {
	SourceID, OwnerAgentID string
	Revision               int64
	Root                   string
	MaxFiles, MaxBytes     int
	HashContent            bool
	Timeout                time.Duration
	Enabled                bool
}
type Event struct {
	SourceID, OwnerAgentID, EventID, Digest string
	Root                                    string
	Files                                   int
	Bytes                                   int64
	ObservedAt                              time.Time
	IntentID                                string
	Generation                              int64
	Data                                    map[string]any
}
type Delivery interface {
	Deliver(context.Context, Event) error
}
type DeliveryFunc func(context.Context, Event) error

func (f DeliveryFunc) Deliver(ctx context.Context, e Event) error { return f(ctx, e) }

type State struct {
	SourceID, OwnerAgentID    string
	Revision                  int64
	Digest, Cursor, PendingID string
	FailureCount              int
	NextRetryAt               time.Time
	Baseline                  bool
	UpdatedAt                 time.Time
	ConfigFingerprint         string
	Occurrence                uint64
	PendingEvent              *Event
	LastError                 string
}

type Store struct {
	mu            sync.Mutex
	path          string
	states        map[string]State
	claims        map[string]chan struct{}
	registrations map[string]SourceRegistration
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, states: map[string]State{}, claims: map[string]chan struct{}{}, registrations: map[string]SourceRegistration{}}
	if strings.TrimSpace(path) == "" {
		return s, nil
	}
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return nil, e
	}
	if len(b) > 0 {
		var envelope struct {
			States        map[string]State              `json:"states"`
			Registrations map[string]SourceRegistration `json:"registrations"`
		}
		if e = json.Unmarshal(b, &envelope); e != nil {
			return nil, e
		}
		if envelope.States == nil && envelope.Registrations == nil { // compatibility with the original state-only format
			if e = json.Unmarshal(b, &s.states); e != nil {
				return nil, e
			}
		} else {
			s.states = envelope.States
			s.registrations = envelope.Registrations
		}
		if s.registrations == nil {
			s.registrations = map[string]SourceRegistration{}
		}
	}
	return s, nil
}

// claim serializes work for one source only. Waiting is cancellable so a slow
// source cannot hold up unrelated sources (or a stopped probe).
func (s *Store) claim(ctx context.Context, source string) error {
	s.mu.Lock()
	if s.claims == nil {
		s.claims = map[string]chan struct{}{}
	}
	c := s.claims[source]
	if c == nil {
		c = make(chan struct{}, 1)
		c <- struct{}{}
		s.claims[source] = c
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c:
		return nil
	}
}
func (s *Store) release(source string) {
	s.mu.Lock()
	c := s.claims[source]
	s.mu.Unlock()
	if c != nil {
		c <- struct{}{}
	}
}
func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, e := json.Marshal(struct {
		States        map[string]State              `json:"states"`
		Registrations map[string]SourceRegistration `json:"registrations,omitempty"`
	}{s.states, s.registrations})
	if e != nil {
		return e
	}
	tmp := s.path + ".tmp"
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, s.path)
}
func (s *Store) Get(source string) (State, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.states[source]
	if v.PendingEvent != nil {
		e := *v.PendingEvent
		v.PendingEvent = &e
	}
	return v, ok
}

func (s *Store) RegisterSource(r SourceRegistration) error {
	if s == nil || strings.TrimSpace(r.SourceID) == "" || strings.TrimSpace(r.OwnerAgentID) == "" || r.Revision <= 0 || strings.TrimSpace(r.Root) == "" || !filepath.IsAbs(r.Root) {
		return ErrOwnerConflict
	}
	r.Root = filepath.Clean(r.Root)
	if r.MaxFiles <= 0 {
		r.MaxFiles = 1000
	}
	if r.MaxBytes <= 0 {
		r.MaxBytes = 4 << 20
	}
	if r.Timeout <= 0 {
		r.Timeout = 5 * time.Second
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.registrations[r.SourceID]; ok {
		if old.OwnerAgentID != r.OwnerAgentID || r.Revision < old.Revision || (r.Revision == old.Revision && !reflect.DeepEqual(old, r)) {
			return ErrOwnerConflict
		}
	}
	old, had := s.registrations[r.SourceID]
	s.registrations[r.SourceID] = r
	if err := s.saveLocked(); err != nil {
		if had {
			s.registrations[r.SourceID] = old
		} else {
			delete(s.registrations, r.SourceID)
		}
		return err
	}
	return nil
}
func (s *Store) GetRegistration(source string) (SourceRegistration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.registrations[source]
	return r, ok
}
func (s *Store) ListRegistrations() []SourceRegistration {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SourceRegistration, 0, len(s.registrations))
	for _, r := range s.registrations {
		out = append(out, r)
	}
	return out
}
func (s *Store) ListRegistrationsForOwner(owner string) []SourceRegistration {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []SourceRegistration{}
	for _, r := range s.registrations {
		if r.OwnerAgentID == owner {
			out = append(out, r)
		}
	}
	return out
}
func (s *Store) RemoveRegistrationCAS(source, owner string, revision int64) error {
	if revision <= 0 {
		return ErrOwnerConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.registrations[source]
	if !ok || old.OwnerAgentID != owner {
		return ErrOwnerConflict
	}
	if revision > 0 && old.Revision != revision {
		return ErrOwnerConflict
	}
	delete(s.registrations, source)
	if err := s.saveLocked(); err != nil {
		s.registrations[source] = old
		return err
	}
	return nil
}
func (s *Store) RemoveRegistration(source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.registrations[source]
	if !ok {
		return nil
	}
	delete(s.registrations, source)
	if err := s.saveLocked(); err != nil {
		s.registrations[source] = old
		return err
	}
	return nil
}

type Probe struct {
	store    *Store
	delivery Delivery
	mu       sync.Mutex
}

func New(store *Store, delivery Delivery) *Probe {
	if store == nil {
		store, _ = OpenStore("")
	}
	return &Probe{store: store, delivery: delivery}
}

// Poll scans only regular files below cfg.Root. Symlinks and junctions are
// rejected when encountered; no command or shell is ever invoked.
func (p *Probe) Poll(ctx context.Context, cfg Config, now time.Time) (State, error) {
	if p == nil || p.store == nil || p.delivery == nil {
		return State{}, fmt.Errorf("event probe unavailable")
	}
	if strings.TrimSpace(cfg.Root) == "" || !filepath.IsAbs(strings.TrimSpace(cfg.Root)) {
		return State{}, fmt.Errorf("probe root must be absolute")
	}
	if cfg.SourceID == "" || cfg.OwnerAgentID == "" || cfg.Revision <= 0 {
		return State{}, ErrOwnerConflict
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = 1000
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 4 << 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	root, err := filepath.Abs(filepath.Clean(cfg.Root))
	if err != nil {
		return State{}, err
	}
	cfp := configFingerprint(cfg, root)
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	if err = p.store.claim(ctx, cfg.SourceID); err != nil {
		return State{}, err
	}
	defer p.store.release(cfg.SourceID)
	old, _ := p.store.Get(cfg.SourceID)
	if old.SourceID != "" && (old.OwnerAgentID != cfg.OwnerAgentID || old.Revision > cfg.Revision || (old.Revision == cfg.Revision && old.ConfigFingerprint != "" && old.ConfigFingerprint != cfp)) {
		return old, ErrOwnerConflict
	}
	if old.LastError != "" && !old.NextRetryAt.IsZero() && now.Before(old.NextRetryAt) {
		return old, ErrRetryBackoff
	}
	if old.PendingID != "" {
		if !old.NextRetryAt.IsZero() && now.Before(old.NextRetryAt) {
			return old, ErrRetryBackoff
		}
		if old.PendingEvent == nil {
			return old, fmt.Errorf("pending event missing")
		}
		if old.PendingEvent.OwnerAgentID != cfg.OwnerAgentID || old.PendingEvent.IntentID != cfg.IntentID || old.PendingEvent.Generation != cfg.Generation {
			return old, ErrOwnerConflict
		}
		if err = p.delivery.Deliver(ctx, *old.PendingEvent); err != nil {
			next := fail(old, now)
			next.LastError = err.Error()
			if pe := p.persist(next); pe != nil {
				return old, errors.Join(err, pe)
			}
			return next, err
		}
		next := old
		next.Cursor = old.PendingID
		next.Digest = old.PendingEvent.Digest
		next.PendingID = ""
		next.PendingEvent = nil
		next.FailureCount = 0
		next.LastError = ""
		next.NextRetryAt = time.Time{}
		next.UpdatedAt = now.UTC()
		if err = p.persist(next); err != nil {
			return old, err
		}
		return next, nil
	}
	digest, files, bytes, err := scan(ctx, root, cfg)
	if err != nil {
		next := old
		next.SourceID = cfg.SourceID
		next.OwnerAgentID = cfg.OwnerAgentID
		next.Revision = cfg.Revision
		next.ConfigFingerprint = cfp
		next = fail(next, now)
		next.LastError = err.Error()
		if pe := p.persist(next); pe != nil {
			return old, errors.Join(err, pe)
		}
		return next, err
	}
	if !eventFilterMatches(cfg.EventFilter, digest, files, bytes) {
		return old, nil
	}
	if !old.Baseline {
		next := State{SourceID: cfg.SourceID, OwnerAgentID: cfg.OwnerAgentID, Revision: cfg.Revision, ConfigFingerprint: cfp, Digest: digest, Cursor: "", Baseline: true, UpdatedAt: now.UTC()}
		if err = p.persist(next); err != nil {
			return State{}, err
		}
		return next, nil
	}
	if old.Digest == digest {
		if old.OwnerAgentID != cfg.OwnerAgentID || old.Revision != cfg.Revision || old.ConfigFingerprint != cfp {
			old.OwnerAgentID, old.Revision, old.ConfigFingerprint, old.UpdatedAt = cfg.OwnerAgentID, cfg.Revision, cfp, now.UTC()
			if err = p.persist(old); err != nil {
				return old, err
			}
		}
		if old.LastError != "" {
			old.LastError = ""
			old.FailureCount = 0
			old.NextRetryAt = time.Time{}
			if err = p.persist(old); err != nil {
				return old, err
			}
		}
		return old, nil
	}
	old.ConfigFingerprint = cfp
	old.OwnerAgentID = cfg.OwnerAgentID
	old.Revision = cfg.Revision
	old.Occurrence++
	e := Event{SourceID: cfg.SourceID, OwnerAgentID: cfg.OwnerAgentID, IntentID: cfg.IntentID, Generation: cfg.Generation, EventID: fmt.Sprintf("%s:%d", cfg.SourceID, old.Occurrence), Digest: digest, Root: root, Files: files, Bytes: bytes, ObservedAt: now.UTC(), Data: map[string]any{"digest": digest, "files": files, "bytes": bytes}}
	old.PendingID = e.EventID
	old.PendingEvent = &e
	old.UpdatedAt = now.UTC()
	if err = p.persist(old); err != nil {
		return State{}, err
	}
	if err = p.delivery.Deliver(ctx, e); err != nil {
		next := fail(old, now)
		next.LastError = err.Error()
		if pe := p.persist(next); pe != nil {
			return old, errors.Join(err, pe)
		}
		return next, err
	}
	old.PendingID = ""
	old.PendingEvent = nil
	old.Cursor = e.EventID
	old.Digest = digest
	old.FailureCount = 0
	old.LastError = ""
	old.NextRetryAt = time.Time{}
	old.UpdatedAt = now.UTC()
	if err = p.persist(old); err != nil {
		return old, err
	}
	return old, nil
}

func (p *Probe) persist(v State) error {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	old, had := p.store.states[v.SourceID]
	p.store.states[v.SourceID] = v
	if err := p.store.saveLocked(); err != nil {
		if had {
			p.store.states[v.SourceID] = old
		} else {
			delete(p.store.states, v.SourceID)
		}
		return err
	}
	return nil
}
func configFingerprint(c Config, root string) string {
	b, _ := json.Marshal([]any{c.SourceID, c.OwnerAgentID, c.Revision, c.IntentID, c.Generation, root, c.MaxFiles, c.MaxBytes, c.HashContent, c.Timeout.String()})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func eventFilterMatches(filter map[string]any, digest string, files int, bytes int64) bool {
	if len(filter) == 0 {
		return true
	}
	eq, ok := filter["equals"].(map[string]any)
	if !ok {
		return false
	}
	data := map[string]any{"digest": digest, "files": files, "bytes": bytes}
	for k, v := range eq {
		if fmt.Sprint(data[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}
func fail(v State, now time.Time) State {
	v.FailureCount++
	d := time.Second * time.Duration(1<<min(v.FailureCount, 8))
	v.NextRetryAt = now.Add(d)
	v.UpdatedAt = now.UTC()
	return v
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func scan(ctx context.Context, root string, c Config) (string, int, int64, error) {
	if strings.TrimSpace(root) == "" {
		return "", 0, 0, fmt.Errorf("probe root is required")
	}
	select {
	case <-ctx.Done():
		return "", 0, 0, ctx.Err()
	default:
	}
	info, e := os.Stat(root)
	if e != nil {
		return "", 0, 0, e
	}
	if !info.IsDir() {
		return "", 0, 0, fmt.Errorf("probe root is not directory")
	}
	resolved, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", 0, 0, e
	}
	select {
	case <-ctx.Done():
		return "", 0, 0, ctx.Err()
	default:
	}
	resolved, _ = filepath.Abs(resolved)
	var rows []string
	var n int
	var entries int
	var total int64
	e = filepath.WalkDir(root, func(path string, d fs.DirEntry, er error) error {
		if er != nil {
			return er
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		entries++
		if entries > c.MaxFiles {
			return fmt.Errorf("probe directory entry limit exceeded")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("probe symlink is not allowed: %s", path)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if n >= c.MaxFiles {
			return fmt.Errorf("probe file limit exceeded")
		}
		fi, x := d.Info()
		if x != nil {
			return x
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel, _ := filepath.Rel(resolved, path)
		var readBytes int64
		if c.HashContent {
			f, x := os.Open(path)
			if x != nil {
				return x
			}
			remaining := int64(c.MaxBytes) - total
			if remaining <= 0 {
				_ = f.Close()
				return fmt.Errorf("probe byte limit exceeded")
			}
			select {
			case <-ctx.Done():
				_ = f.Close()
				return ctx.Err()
			default:
			}
			b, x := io.ReadAll(io.LimitReader(f, remaining+1))
			_ = f.Close()
			if x != nil {
				return x
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if int64(len(b)) > remaining {
				return fmt.Errorf("probe byte limit exceeded")
			}
			readBytes = int64(len(b))
			h := sha256.Sum256(b)
			rows = append(rows, fmt.Sprintf("%s:%d:%s", rel, fi.Size(), hex.EncodeToString(h[:])))
		} else {
			remaining := int64(c.MaxBytes) - total
			if fi.Size() > remaining {
				return fmt.Errorf("probe byte limit exceeded")
			}
			rows = append(rows, fmt.Sprintf("%s:%d:%d", rel, fi.Size(), fi.ModTime().UnixNano()))
		}
		n++
		if c.HashContent {
			total += readBytes
		} else {
			total += fi.Size()
		}
		return nil
	})
	if e != nil {
		return "", 0, 0, e
	}
	sort.Strings(rows)
	h := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(h[:]), n, total, nil
}
