package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeSpec_defaults(t *testing.T) {
	s := NormalizeSpec(Spec{})
	if s.Image != DefaultImage {
		t.Fatalf("image=%q", s.Image)
	}
	if s.Network != "none" {
		t.Fatalf("network=%q", s.Network)
	}
}

func TestAvailable_missingDocker(t *testing.T) {
	old := lookPath
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { lookPath = old })
	if err := Available(); err == nil {
		t.Fatal("expected error")
	}
}

func TestDockerRunner_EnsureAndExecArgs(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	restore := SetRunDockerForTest(func(_ context.Context, bin string, args ...string) (string, string, error) {
		calls = append(calls, append([]string{bin}, args...))
		if len(args) >= 1 && args[0] == "inspect" {
			// 首次 Ensure 前不存在
			if len(calls) <= 2 {
				return "false", "", nil
			}
			return "true", "", nil
		}
		return "", "", nil
	})
	t.Cleanup(restore)

	r, err := NewDockerRunner("agt-1", root, Spec{Image: "alpine:3.20", Network: "none", Memory: "256m", CPUs: "0.5"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, c := range calls {
		joined += strings.Join(c, " ") + "\n"
	}
	if !strings.Contains(joined, "create --name dagents-sbx-agt-1") {
		t.Fatalf("create missing: %s", joined)
	}
	if !strings.Contains(joined, "start dagents-sbx-agt-1") {
		t.Fatalf("start missing: %s", joined)
	}
	if !strings.Contains(joined, "-v "+root+":/workspace:rw") {
		t.Fatalf("volume missing: %s", joined)
	}
	if !strings.Contains(joined, "sleep infinity") {
		t.Fatalf("keepalive missing: %s", joined)
	}

	args, err := r.BuildExecArgs(sub, "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	execJoined := strings.Join(args, " ")
	if !strings.Contains(execJoined, "exec -i") || !strings.Contains(execJoined, "-w /workspace/src") {
		t.Fatalf("exec args=%v", args)
	}
	if args[len(args)-1] != "echo hi" {
		t.Fatalf("command=%v", args)
	}
}

func TestDockerRunner_cwdEscape(t *testing.T) {
	root := t.TempDir()
	r, err := NewDockerRunner("agt-x", root, Spec{})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside")
	_, err = r.BuildExecArgs(outside, "true")
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestDockerRunner_IdleExpired(t *testing.T) {
	root := t.TempDir()
	restore := SetRunDockerForTest(func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	})
	t.Cleanup(restore)
	r, err := NewDockerRunner("agt-idle", root, Spec{})
	if err != nil {
		t.Fatal(err)
	}
	r.IdleTimeout = time.Minute
	if err := r.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.IdleExpired(time.Now()) {
		t.Fatal("should not be idle immediately")
	}
	r.mu.Lock()
	r.lastUsed = time.Now().Add(-2 * time.Minute)
	r.mu.Unlock()
	if !r.IdleExpired(time.Now()) {
		t.Fatal("expected idle")
	}
}

func TestPool_ReleaseAndReap(t *testing.T) {
	root := t.TempDir()
	var removed []string
	restore := SetRunDockerForTest(func(_ context.Context, _ string, args ...string) (string, string, error) {
		if len(args) >= 1 && args[0] == "rm" {
			removed = append(removed, args[len(args)-1])
		}
		return "", "", nil
	})
	t.Cleanup(restore)

	p := NewPool(time.Minute, nil)
	r, err := NewDockerRunner("agt-pool", root, Spec{})
	if err != nil {
		t.Fatal(err)
	}
	r.IdleTimeout = time.Minute
	if err := p.Ensure(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.lastUsed = time.Now().Add(-2 * time.Minute)
	r.mu.Unlock()
	p.reapIdle(time.Now())
	if len(removed) == 0 {
		t.Fatal("expected idle rm")
	}
	p.Release("agt-pool")
}

func TestContainerName(t *testing.T) {
	if got := ContainerName("agt-abc/../x"); !strings.HasPrefix(got, "dagents-sbx-") {
		t.Fatalf("got=%q", got)
	}
}
