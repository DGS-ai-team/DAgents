package shelllog

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/logfiles"
)

func TestSetupWritesDatedShellLog(t *testing.T) {
	dir := t.TempDir()
	closer, err := Setup(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	logPath := logfiles.JoinDated(filepath.Join(dir, ".runtime", "logs"), "shell", false, time.Now())
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
	// 旧固定名不应再作为主日志。
	if _, err := os.Stat(filepath.Join(dir, ".runtime", "logs", "shell.log")); err == nil {
		t.Fatal("unexpected undated shell.log")
	}
}
