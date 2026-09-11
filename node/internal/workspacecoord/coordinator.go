// Package workspacecoord serializes writes to overlapping local workspaces.
package workspacecoord

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type Lease struct {
	c    *Coordinator
	id   uint64
	once sync.Once
}

type heldLease struct {
	id          uint64
	root, owner string
}

type Coordinator struct {
	mu   sync.Mutex
	wake chan struct{}
	next uint64
	held map[uint64]heldLease
}

func New() *Coordinator {
	return &Coordinator{wake: make(chan struct{}), held: make(map[uint64]heldLease)}
}

// Canonical resolves an absolute local path, including symlinks in existing
// parents. The unresolved suffix is retained for a not-yet-created target.
func Canonical(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !filepath.IsAbs(raw) {
		return "", fmt.Errorf("workspace path must be absolute")
	}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", fmt.Errorf("workspace path contains parent traversal")
		}
	}
	p, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", err
	}
	cur := p
	var suffix []string
	for {
		if _, err = os.Lstat(cur); err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve workspace path: %w", err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("resolve workspace path: no existing parent")
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
	cur, err = filepath.EvalSymlinks(cur)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		cur = filepath.Join(cur, suffix[i])
	}
	return filepath.Abs(filepath.Clean(cur))
}

func comparable(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(filepath.Clean(p))
	}
	return filepath.Clean(p)
}
func conflict(a, b string) bool {
	a, b = comparable(a), comparable(b)
	return a == b || pathParent(a, b) || pathParent(b, a)
}
func pathParent(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

// TryAcquire waits until no held write lease covers root or one of its
// ancestors. Reads should not call this method in the first implementation.
func (c *Coordinator) TryAcquire(ctx context.Context, root, owner string) (*Lease, error) {
	if c == nil {
		return nil, fmt.Errorf("workspace coordinator unavailable")
	}
	canonical, err := Canonical(root)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		busy := false
		for _, h := range c.held {
			if conflict(h.root, canonical) {
				busy = true
				break
			}
		}
		if !busy {
			if err := ctx.Err(); err != nil {
				c.mu.Unlock()
				return nil, err
			}
			c.next++
			id := c.next
			c.held[id] = heldLease{id: id, root: canonical, owner: owner}
			c.mu.Unlock()
			return &Lease{c: c, id: id}, nil
		}
		wake := c.wake
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wake:
		}
	}
}

// Release is idempotent; an old lease token can never release a later lease.
func (l *Lease) Release() {
	if l == nil || l.c == nil {
		return
	}
	l.once.Do(func() {
		l.c.mu.Lock()
		if _, ok := l.c.held[l.id]; ok {
			delete(l.c.held, l.id)
			old := l.c.wake
			l.c.wake = make(chan struct{})
			close(old)
		}
		l.c.mu.Unlock()
	})
}
