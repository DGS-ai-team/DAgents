package shelllog

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupWritesShellLog(t *testing.T) {
	dir := t.TempDir()
	closer, err := Setup(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	logPath := filepath.Join(dir, ".runtime", "logs", "shell.log")
	log.SetPrefix("test ")
	log.Printf("hello shell")
	_ = closer.Close()

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hello shell") {
		t.Fatalf("log = %q", string(raw))
	}
}
