// Package handbookfs provides the filesystem-backed history for an Agent
// handbook. Its private .history directory is intentionally outside the
// model-facing handbook namespace.
package handbookfs

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var ErrConflict = errors.New("handbook file changed since receipt")

type Entry struct {
	Revision     int64     `json:"revision"`
	Path         string    `json:"path"`
	BeforeDigest string    `json:"before_digest,omitempty"`
	AfterDigest  string    `json:"after_digest"`
	BeforeExists bool      `json:"before_exists"`
	AfterExists  bool      `json:"after_exists"`
	CreatedAt    time.Time `json:"created_at"`
}

type pendingTxn struct {
	Revision     int64  `json:"revision"`
	Path         string `json:"path"`
	Before       []byte `json:"before"`
	BeforeExists bool   `json:"before_exists"`
	AfterDigest  string `json:"after_digest"`
	AfterExists  bool   `json:"after_exists"`
}

type Service struct {
	root string
	mu   *sync.Mutex
}

var rootsMu sync.Mutex
var roots = map[string]*sync.Mutex{}

func New(root string) (*Service, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, errors.New("handbook root required")
	}
	if err := os.MkdirAll(filepath.Join(root, ".history", "snapshots"), 0700); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if real, e := filepath.EvalSymlinks(root); e == nil {
		root = real
	}
	lockRoot := root
	if runtime.GOOS == "windows" {
		lockRoot = strings.ToLower(lockRoot)
	}
	rootsMu.Lock()
	mu := roots[lockRoot]
	if mu == nil {
		mu = &sync.Mutex{}
		roots[lockRoot] = mu
	}
	rootsMu.Unlock()
	s := &Service{root: root, mu: mu}
	s.mu.Lock()
	err = s.recoverPendingLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s, nil
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Service) DigestFile(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

// Write performs digest validation, snapshots the old bytes, atomically
// replaces the file, and commits the manifest under one shared root lock.
func (s *Service) Write(ctx context.Context, path, expectedDigest string, after []byte) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	if _, err := s.relative(path); err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverPendingLocked(); err != nil {
		return Entry{}, err
	}
	current, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		current = nil
	} else if err != nil {
		return Entry{}, err
	}
	if strings.TrimSpace(expectedDigest) != "" && Digest(current) != strings.TrimSpace(expectedDigest) {
		return Entry{}, ErrConflict
	}
	entries, err := s.readEntriesLocked()
	if err != nil {
		return Entry{}, err
	}
	e, err := s.prepareEntryLocked(path, current, after, entries)
	if err != nil {
		return Entry{}, err
	}
	tmp := path + fmt.Sprintf(".handbook-tmp-%d", time.Now().UnixNano())
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return Entry{}, err
	}
	if err := s.writePending(pendingTxn{Revision: e.Revision, Path: path, Before: current, BeforeExists: current != nil, AfterDigest: Digest(after), AfterExists: true}); err != nil {
		return Entry{}, err
	}
	if err := os.WriteFile(tmp, after, 0644); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return Entry{}, err
	}
	if err := s.appendEntryLocked(e); err != nil {
		if current == nil {
			_ = os.Remove(path)
		} else {
			_ = os.WriteFile(path, current, 0644)
		}
		return Entry{}, err
	}
	_ = os.Remove(s.pendingPath())
	return e, nil
}

func (s *Service) History(ctx context.Context, path string) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rel, err := s.relative(path)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverPendingLocked(); err != nil {
		return nil, err
	}
	entries, err := s.readEntriesLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0)
	for _, e := range entries {
		if e.Path == rel {
			out = append(out, e)
		}
	}
	return out, nil
}

// Restore writes the prior bytes from revision and records the restore as a
// new revision, so recovery itself remains auditable.
func (s *Service) Restore(ctx context.Context, path, expectedDigest string, revision int64) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	rel, err := s.relative(path)
	if err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverPendingLocked(); err != nil {
		return Entry{}, err
	}
	entries, err := s.readEntriesLocked()
	if err != nil {
		return Entry{}, err
	}
	var found Entry
	for _, e := range entries {
		if e.Path == rel && e.Revision == revision {
			found = e
			break
		}
	}
	if found.Revision == 0 {
		return Entry{}, os.ErrNotExist
	}
	current, err := os.ReadFile(path)
	currentExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return Entry{}, err
	}
	if err != nil {
		current = nil
	}
	old := append([]byte{}, current...)
	if !currentExists {
		old = nil
	}
	if strings.TrimSpace(expectedDigest) != "" && Digest(old) != strings.TrimSpace(expectedDigest) {
		return Entry{}, ErrConflict
	}
	before, err := os.ReadFile(filepath.Join(s.root, ".history", "snapshots", fmt.Sprintf("%020d.before", revision)))
	if err != nil {
		return Entry{}, err
	}
	// Restore through the same atomic commit path. The selected revision's
	// preimage is the requested content; a first-write preimage means delete.
	var after []byte
	if found.BeforeExists {
		after = before
	}
	e, err := s.prepareEntryLocked(path, old, after, entries)
	if err != nil {
		return Entry{}, err
	}
	if err := s.writePending(pendingTxn{Revision: e.Revision, Path: path, Before: old, BeforeExists: currentExists, AfterDigest: Digest(after), AfterExists: after != nil}); err != nil {
		return Entry{}, err
	}
	if found.BeforeExists {
		tmp := path + fmt.Sprintf(".handbook-tmp-%d", time.Now().UnixNano())
		if err := os.WriteFile(tmp, after, 0644); err != nil {
			return Entry{}, err
		}
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			return Entry{}, err
		}
	} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return Entry{}, err
	}
	if err := s.appendEntryLocked(e); err != nil {
		if old == nil {
			_ = os.Remove(path)
		} else {
			_ = os.WriteFile(path, old, 0644)
		}
		return Entry{}, err
	}
	_ = os.Remove(s.pendingPath())
	return e, nil
}

func (s *Service) prepareEntryLocked(path string, before, after []byte, entries []Entry) (Entry, error) {
	rel, err := s.relative(path)
	if err != nil {
		return Entry{}, err
	}
	var rev int64
	for _, e := range entries {
		if e.Revision > rev {
			rev = e.Revision
		}
	}
	rev++
	e := Entry{Revision: rev, Path: rel, BeforeDigest: Digest(before), AfterDigest: Digest(after), BeforeExists: before != nil, AfterExists: true, CreatedAt: time.Now().UTC()}
	if after == nil {
		e.AfterExists = false
	}
	if err := os.WriteFile(filepath.Join(s.root, ".history", "snapshots", fmt.Sprintf("%020d.before", rev)), func() []byte {
		if before == nil {
			return []byte{}
		}
		return before
	}(), 0600); err != nil {
		return Entry{}, err
	}
	return e, nil
}

func (s *Service) relative(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	probe := abs
	var tail []string
	for {
		if _, e := os.Lstat(probe); e == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		tail = append(tail, filepath.Base(probe))
		probe = parent
	}
	if real, e := filepath.EvalSymlinks(probe); e == nil {
		abs = real
		for i := len(tail) - 1; i >= 0; i-- {
			abs = filepath.Join(abs, tail[i])
		}
	}
	rel, err := filepath.Rel(s.root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("path outside handbook root")
	}
	probe = abs
	for {
		if _, statErr := os.Lstat(probe); statErr == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	if real, e := filepath.EvalSymlinks(probe); e == nil {
		real, _ = filepath.Abs(real)
		if !pathWithin(s.root, real) {
			return "", errors.New("path symlink escapes handbook root")
		}
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == ".history" {
			return "", errors.New("handbook history is private")
		}
	}
	return filepath.ToSlash(rel), nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (s *Service) manifest() string { return filepath.Join(s.root, ".history", "manifest.jsonl") }
func (s *Service) readEntriesLocked() ([]Entry, error) {
	f, err := os.Open(s.manifest())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, sc.Err()
}
func (s *Service) appendEntryLocked(e Entry) error {
	f, err := os.OpenFile(s.manifest(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(e)
	_, err = f.Write(append(b, '\n'))
	return err
}

func (s *Service) pendingPath() string { return filepath.Join(s.root, ".history", "pending.json") }
func (s *Service) writePending(p pendingTxn) error {
	b, _ := json.Marshal(p)
	return os.WriteFile(s.pendingPath(), b, 0600)
}
func (s *Service) recoverPendingLocked() error {
	b, err := os.ReadFile(s.pendingPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var p pendingTxn
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if p.Revision <= 0 {
		return fmt.Errorf("invalid pending handbook revision")
	}
	pendingRel, err := s.relative(p.Path)
	if err != nil {
		return fmt.Errorf("invalid pending handbook path: %w", err)
	}
	entries, err := s.readEntriesLocked()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Revision == p.Revision && e.Path == pendingRel && e.AfterDigest == p.AfterDigest && e.AfterExists == p.AfterExists {
			return os.Remove(s.pendingPath())
		}
	}
	current, readErr := os.ReadFile(p.Path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if readErr == nil && p.AfterDigest != "" && Digest(current) != p.AfterDigest && Digest(current) != Digest(p.Before) {
		return fmt.Errorf("pending handbook write found external change; manual recovery required")
	}
	if readErr != nil && p.BeforeExists && p.AfterExists {
		return fmt.Errorf("pending handbook write state is ambiguous; manual recovery required")
	}
	if readErr == nil && Digest(current) == Digest(p.Before) {
		return os.Remove(s.pendingPath())
	}
	if readErr != nil && !p.AfterExists { // expected delete completed; restore old bytes
		if p.BeforeExists {
			err = os.WriteFile(p.Path, p.Before, 0644)
		} else {
			err = nil
		}
		if err != nil {
			return err
		}
		return os.Remove(s.pendingPath())
	}
	if p.BeforeExists {
		err = os.WriteFile(p.Path, p.Before, 0644)
	} else {
		err = os.Remove(p.Path)
		if os.IsNotExist(err) {
			err = nil
		}
	}
	if err != nil {
		return fmt.Errorf("recover pending handbook write: %w", err)
	}
	return os.Remove(s.pendingPath())
}
