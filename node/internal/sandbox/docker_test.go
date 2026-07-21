package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestAvailable_ok(t *testing.T) {
	old := lookPath
	lookPath = func(string) (string, error) { return "/usr/bin/docker", nil }
	t.Cleanup(func() { lookPath = old })
	if err := Available(); err != nil {
		t.Fatal(err)
	}
}

func TestDockerRunner_BuildRunArgs(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := NewDockerRunner(root, Spec{Image: "alpine:3.20", Network: "none", Memory: "256m", CPUs: "0.5"})
	if err != nil {
		t.Fatal(err)
	}
	args, err := r.BuildRunArgs(sub, "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "docker run --rm -i") {
		t.Fatalf("args=%v", args)
	}
	if !strings.Contains(joined, "-v "+root+":/workspace:rw") {
		t.Fatalf("missing volume: %v", args)
	}
	if !strings.Contains(joined, "-w /workspace/src") {
		t.Fatalf("missing -w: %v", args)
	}
	if !strings.Contains(joined, "--memory 256m") || !strings.Contains(joined, "--cpus 0.5") {
		t.Fatalf("missing limits: %v", args)
	}
	if args[len(args)-3] != "bash" || args[len(args)-2] != "-lc" || args[len(args)-1] != "echo hi" {
		t.Fatalf("tail=%v", args[len(args)-3:])
	}
	foundImage := false
	for _, a := range args {
		if a == "alpine:3.20" {
			foundImage = true
			break
		}
	}
	if !foundImage {
		t.Fatalf("missing image in %v", args)
	}
}

func TestDockerRunner_cwdEscape(t *testing.T) {
	root := t.TempDir()
	r, err := NewDockerRunner(root, Spec{})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside")
	_, err = r.BuildRunArgs(outside, "true")
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestContainerName(t *testing.T) {
	if got := ContainerName("agt-abc/../x"); !strings.HasPrefix(got, "dagents-sbx-") {
		t.Fatalf("got=%q", got)
	}
}
