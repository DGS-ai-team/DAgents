package events

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type blockingDelivery struct {
	mu          sync.Mutex
	blockSource string
	entered     chan struct{}
	release     chan struct{}
}

func (d *blockingDelivery) Deliver(ctx context.Context, e Event) error {
	if e.SourceID != d.blockSource {
		return nil
	}
	select {
	case d.entered <- struct{}{}:
	default:
	}
	select {
	case <-d.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type fakeDelivery struct {
	calls int
	fail  bool
}

func (d *fakeDelivery) Deliver(context.Context, Event) error {
	d.calls++
	if d.fail {
		return os.ErrPermission
	}
	return nil
}
func cfg(root string) Config {
	return Config{SourceID: "files", OwnerAgentID: "agent", Revision: 1, Root: root, MaxFiles: 10, MaxBytes: 1024, HashContent: true}
}

func TestProbeBaselineChangeAndNoChange(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("one"), 0600)
	d := &fakeDelivery{}
	p := New(nil, d)
	now := time.Now().UTC()
	s, err := p.Poll(context.Background(), cfg(root), now)
	if err != nil || !s.Baseline || d.calls != 0 {
		t.Fatalf("baseline=%+v err=%v calls=%d", s, err, d.calls)
	}
	if _, err = p.Poll(context.Background(), cfg(root), now.Add(time.Second)); err != nil || d.calls != 0 {
		t.Fatalf("unchanged err=%v calls=%d", err, d.calls)
	}
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("two"), 0600)
	if s, err = p.Poll(context.Background(), cfg(root), now.Add(2*time.Second)); err != nil || d.calls != 1 || s.PendingID != "" {
		t.Fatalf("change=%+v err=%v calls=%d", s, err, d.calls)
	}
}

func TestProbeReopenPersistsCursorAndFailureBackoff(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "events.json")
	os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0600)
	d := &fakeDelivery{}
	p := New(mustStore(t, path), d)
	now := time.Now().UTC()
	_, _ = p.Poll(context.Background(), cfg(root), now)
	os.WriteFile(filepath.Join(root, "a"), []byte("b"), 0600)
	d.fail = true
	if _, err := p.Poll(context.Background(), cfg(root), now.Add(time.Second)); err == nil {
		t.Fatal("expected delivery failure")
	}
	st, _ := p.store.Get("files")
	if st.PendingID == "" || st.FailureCount != 1 || st.NextRetryAt.IsZero() {
		t.Fatalf("failure state=%+v", st)
	}
	if _, err := p.Poll(context.Background(), cfg(root), now.Add(2*time.Second)); err != ErrRetryBackoff {
		t.Fatalf("backoff err=%v", err)
	}
	d.fail = false
	p2 := New(mustStore(t, path), d)
	if _, err := p2.Poll(context.Background(), cfg(root), st.NextRetryAt.Add(time.Second)); err != nil || d.calls != 2 {
		t.Fatalf("reopen retry err=%v calls=%d", err, d.calls)
	}
}

func TestProbeOwnerCASAndBounds(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0600)
	d := &fakeDelivery{}
	p := New(nil, d)
	now := time.Now().UTC()
	_, _ = p.Poll(context.Background(), cfg(root), now)
	bad := cfg(root)
	bad.OwnerAgentID = "other"
	if _, err := p.Poll(context.Background(), bad, now.Add(time.Second)); err != ErrOwnerConflict {
		t.Fatalf("owner err=%v", err)
	}
	link := filepath.Join(root, "link")
	if os.Symlink(filepath.Join(root, "a"), link) == nil {
		if _, err := p.Poll(context.Background(), cfg(root), now.Add(2*time.Second)); err == nil {
			t.Fatal("symlink accepted")
		}
	}
}

func TestProbePendingReplaySurvivesRootRemoval(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0600)
	d := &fakeDelivery{fail: true}
	p := New(nil, d)
	now := time.Now().UTC()
	_, _ = p.Poll(context.Background(), cfg(root), now)
	os.WriteFile(filepath.Join(root, "a"), []byte("b"), 0600)
	_, _ = p.Poll(context.Background(), cfg(root), now.Add(time.Second))
	st, _ := p.store.Get("files")
	os.RemoveAll(root)
	d.fail = false
	if _, err := p.Poll(context.Background(), cfg(root), st.NextRetryAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}
func TestProbePendingEventIsImmutableAcrossChanges(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0600)
	d := &fakeDelivery{fail: true}
	p := New(nil, d)
	now := time.Now().UTC()
	_, _ = p.Poll(context.Background(), cfg(root), now)
	os.WriteFile(filepath.Join(root, "a"), []byte("b"), 0600)
	_, _ = p.Poll(context.Background(), cfg(root), now.Add(time.Second))
	pending, _ := p.store.Get("files")
	os.WriteFile(filepath.Join(root, "a"), []byte("c"), 0600)
	d.fail = false
	if _, err := p.Poll(context.Background(), cfg(root), pending.NextRetryAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if d.calls != 2 {
		t.Fatalf("calls=%d", d.calls)
	}
}
func TestProbeConcurrentProbesSingleDelivery(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0600)
	d := &fakeDelivery{}
	s := mustStore(t, filepath.Join(t.TempDir(), "s.json"))
	p1, p2 := New(s, d), New(s, d)
	now := time.Now().UTC()
	_, _ = p1.Poll(context.Background(), cfg(root), now)
	os.WriteFile(filepath.Join(root, "a"), []byte("b"), 0600)
	ch := make(chan error, 2)
	go func() { _, e := p1.Poll(context.Background(), cfg(root), now.Add(time.Second)); ch <- e }()
	go func() { _, e := p2.Poll(context.Background(), cfg(root), now.Add(time.Second)); ch <- e }()
	<-ch
	<-ch
	if d.calls != 1 {
		t.Fatalf("calls=%d", d.calls)
	}
}
func TestProbeDigestAtoBAtoAProducesNewOccurrences(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a")
	os.WriteFile(path, []byte("a"), 0600)
	d := &fakeDelivery{}
	p := New(nil, d)
	now := time.Now().UTC()
	_, _ = p.Poll(context.Background(), cfg(root), now)
	os.WriteFile(path, []byte("b"), 0600)
	_, _ = p.Poll(context.Background(), cfg(root), now.Add(time.Second))
	os.WriteFile(path, []byte("a"), 0600)
	st, err := p.Poll(context.Background(), cfg(root), now.Add(2*time.Second))
	if err != nil || st.Occurrence != 2 || d.calls != 2 {
		t.Fatalf("state=%+v err=%v calls=%d", st, err, d.calls)
	}
}
func TestProbeConfigCASRejectsSameRevisionRootChange(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	d := &fakeDelivery{}
	p := New(nil, d)
	now := time.Now().UTC()
	if _, err := p.Poll(context.Background(), cfg(a), now); err != nil {
		t.Fatal(err)
	}
	c := cfg(b)
	if _, err := p.Poll(context.Background(), c, now.Add(time.Second)); err != ErrOwnerConflict {
		t.Fatalf("err=%v", err)
	}
}
func TestProbePersistFailureRollsBackState(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0600)
	d := &fakeDelivery{}
	p := New(nil, d)
	now := time.Now().UTC()
	p.store.path = t.TempDir()
	if _, err := p.Poll(context.Background(), cfg(root), now); err == nil {
		t.Fatal("expected persist failure")
	}
	if _, ok := p.store.Get("files"); ok {
		t.Fatal("state persisted after failure")
	}
}
func mustStore(t *testing.T, path string) *Store {
	s, e := OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestProbeSourcesDoNotBlockEachOther(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(a, "a"), []byte("a"), 0600)
	os.WriteFile(filepath.Join(b, "b"), []byte("a"), 0600)
	d := &blockingDelivery{blockSource: "a", entered: make(chan struct{}, 1), release: make(chan struct{})}
	p := New(nil, d)
	ca, cb := cfg(a), cfg(b)
	ca.SourceID = "a"
	cb.SourceID = "b"
	p.Poll(context.Background(), ca, time.Now())
	p.Poll(context.Background(), cb, time.Now())
	os.WriteFile(filepath.Join(a, "a"), []byte("b"), 0600)
	os.WriteFile(filepath.Join(b, "b"), []byte("b"), 0600)
	done := make(chan error, 1)
	go func() { _, e := p.Poll(context.Background(), ca, time.Now().Add(time.Second)); done <- e }()
	<-d.entered
	if _, err := p.Poll(context.Background(), cb, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("source B blocked: %v", err)
	}
	close(d.release)
	<-done
}

func TestProbeSameSourceClaimHonorsContext(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0600)
	d := &blockingDelivery{blockSource: "files", entered: make(chan struct{}, 1), release: make(chan struct{})}
	p := New(nil, d)
	c := cfg(root)
	p.Poll(context.Background(), c, time.Now())
	os.WriteFile(filepath.Join(root, "a"), []byte("b"), 0600)
	go p.Poll(context.Background(), c, time.Now().Add(time.Second))
	<-d.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := p.Poll(ctx, c, time.Now().Add(time.Second))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	close(d.release)
}

func TestProbeHigherRevisionPersistsAndOldRevisionRejected(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0600)
	path := filepath.Join(t.TempDir(), "s.json")
	p := New(mustStore(t, path), &fakeDelivery{})
	c := cfg(root)
	now := time.Now()
	p.Poll(context.Background(), c, now)
	c.Revision = 2
	s, err := p.Poll(context.Background(), c, now.Add(time.Second))
	if err != nil || s.Revision != 2 {
		t.Fatalf("upgrade=%+v %v", s, err)
	}
	if s.ConfigFingerprint == "" {
		t.Fatal("missing fingerprint")
	}
	c.Revision = 1
	if _, err = p.Poll(context.Background(), c, now.Add(2*time.Second)); err != ErrOwnerConflict {
		t.Fatalf("old revision err=%v", err)
	}
	reopened := New(mustStore(t, path), &fakeDelivery{})
	got, _ := reopened.store.Get("files")
	if got.Revision != 2 {
		t.Fatalf("persisted=%+v", got)
	}
}

func TestProbeDeliveryAndPersistErrorsAreJoined(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0600)
	p := New(nil, &fakeDelivery{})
	c := cfg(root)
	p.Poll(context.Background(), c, time.Now())
	os.WriteFile(filepath.Join(root, "a"), []byte("b"), 0600)
	p.store.path = t.TempDir()
	d := &fakeDelivery{fail: true}
	p.delivery = d
	_, err := p.Poll(context.Background(), c, time.Now().Add(time.Second))
	if err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("joined err=%v", err)
	}
	s, _ := p.store.Get(c.SourceID)
	if s.FailureCount != 0 {
		t.Fatalf("state not rolled back: %+v", s)
	}
}

func TestProbeDirectoryEnumerationLimit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 4; i++ {
		os.Mkdir(filepath.Join(root, string(rune('a'+i))), 0700)
	}
	c := cfg(root)
	c.MaxFiles = 3
	if _, _, _, err := scan(context.Background(), root, c); err == nil {
		t.Fatal("enumeration limit not enforced")
	}
}

func TestSourceRegistrationIsOwnerScopedAndDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	s := mustStore(t, path)
	r := SourceRegistration{SourceID: "files", OwnerAgentID: "agent", Revision: 3, Root: t.TempDir(), Enabled: true}
	if err := s.RegisterSource(r); err != nil {
		t.Fatal(err)
	}
	got, ok := mustStore(t, path).GetRegistration("files")
	if !ok || got.OwnerAgentID != r.OwnerAgentID || got.Revision != r.Revision || !got.Enabled {
		t.Fatalf("registration=%+v ok=%v", got, ok)
	}
}
