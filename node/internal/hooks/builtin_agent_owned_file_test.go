package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type fakePathStater struct {
	files map[string]time.Time
}

func (f *fakePathStater) StatRelPath(relPath string) (bool, time.Time, error) {
	key := NormalizePathKey(relPath)
	if mt, ok := f.files[key]; ok {
		return true, mt, nil
	}
	return false, time.Time{}, nil
}

func (f *fakePathStater) set(relPath string, mtime time.Time) {
	if f.files == nil {
		f.files = make(map[string]time.Time)
	}
	f.files[NormalizePathKey(relPath)] = mtime
}

func TestAgentOwnedFileHook_trustHitAuto(t *testing.T) {
	mt := time.Unix(100, 0)
	stater := &fakePathStater{files: map[string]time.Time{"a.txt": mt}}
	trust := NewAgentFileTrust()
	trust.MarkOwned("a.txt", mt)

	hook := NewAgentOwnedFileHook(AgentOwnedFileConfig{Enabled: true, PathStater: stater})
	hook.SetTrust(trust)

	out := ToolBeforeEachResult{
		ToolMode: policy.ModeRule,
		Action:   policy.ActionRequireApproval,
	}
	err := hook.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{
		ToolName: "search_replace",
		ToolArgs: map[string]any{"path": "a.txt", "old_string": "x", "new_string": "y"},
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != policy.ActionAuto {
		t.Fatalf("Action = %q, want auto", out.Action)
	}
}

func TestAgentOwnedFileHook_mtimeMismatchKeepsApproval(t *testing.T) {
	stored := time.Unix(100, 0)
	disk := time.Unix(200, 0)
	stater := &fakePathStater{files: map[string]time.Time{"a.txt": disk}}
	trust := NewAgentFileTrust()
	trust.MarkOwned("a.txt", stored)

	hook := NewAgentOwnedFileHook(AgentOwnedFileConfig{Enabled: true, PathStater: stater})
	hook.SetTrust(trust)

	out := ToolBeforeEachResult{
		ToolMode: policy.ModeRule,
		Action:   policy.ActionRequireApproval,
	}
	_ = hook.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{
		ToolName: "write_file",
		ToolArgs: map[string]any{"path": "a.txt", "content": "hi"},
	}, &out)
	if out.Action != policy.ActionRequireApproval {
		t.Fatalf("Action = %q", out.Action)
	}
}

func TestAgentOwnedFileHook_alwaysModeSkipped(t *testing.T) {
	mt := time.Unix(100, 0)
	stater := &fakePathStater{files: map[string]time.Time{"a.txt": mt}}
	trust := NewAgentFileTrust()
	trust.MarkOwned("a.txt", mt)

	hook := NewAgentOwnedFileHook(AgentOwnedFileConfig{Enabled: true, PathStater: stater})
	hook.SetTrust(trust)

	out := ToolBeforeEachResult{
		ToolMode: policy.ModeAlways,
		Action:   policy.ActionRequireApproval,
	}
	_ = hook.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{
		ToolName: "write_file",
		ToolArgs: map[string]any{"path": "a.txt", "content": "hi"},
	}, &out)
	if out.Action != policy.ActionRequireApproval {
		t.Fatalf("always mode must not downgrade, got %q", out.Action)
	}
}

func TestAgentOwnedFileHook_writeFileENOENTSetsPending(t *testing.T) {
	stater := &fakePathStater{}
	trust := NewAgentFileTrust()
	hook := NewAgentOwnedFileHook(AgentOwnedFileConfig{Enabled: true, PathStater: stater})
	hook.SetTrust(trust)

	out := ToolBeforeEachResult{ToolMode: policy.ModeRule, Action: policy.ActionRequireApproval}
	_ = hook.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{
		ToolName: "write_file",
		ToolArgs: map[string]any{"path": "new.txt", "content": "hi"},
	}, &out)
	if !trust.ConsumePendingCreate("new.txt") {
		t.Fatal("expected pending create flag")
	}
}

func TestAgentOwnedFileAfterHook_marksOwnedOnCreate(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	trust := NewAgentFileTrust()
	trust.SetPendingCreate("created.txt")

	after := NewAgentOwnedFileAfterHook(AgentOwnedFileConfig{Enabled: true, PathStater: reg})
	after.SetTrust(trust)

	if _, err := reg.Execute(context.Background(), "write_file", `{"path":"created.txt","content":"body","call_purpose":"t"}`); err != nil {
		t.Fatal(err)
	}

	var out ToolAfterEachOutput
	if err := after.RunToolAfterEach(context.Background(), ToolAfterEachInput{
		ToolName:     "write_file",
		RawArguments: `{"path":"created.txt","content":"body","call_purpose":"t"}`,
		RawResult:    "wrote 4 bytes",
	}, &out); err != nil {
		t.Fatal(err)
	}
	if !trust.IsOwned("created.txt") {
		t.Fatal("expected owned after create")
	}
}

func TestRegistryAgentOwnedTrustChain(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	dir := writeTestPolicyDir(t, "write_file=rule\nsearch_replace=rule\n", "")
	engine, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	trust := NewAgentFileTrust()
	hooksReg := NewRegistry(engine, RuntimeConfig{
		Duplicate:      DefaultDuplicateConfig(),
		AgentOwnedFile: AgentOwnedFileConfig{Enabled: true, PathStater: reg},
	})
	hooksReg.SetAgentFileTrust(trust)

	// first write: require approval
	out := registryToolBeforeEach(hooksReg, ToolBeforeEachInput{
		ToolName: "write_file",
		ToolArgs: map[string]any{"path": "x.txt", "content": "a"},
	})
	if out.Action != policy.ActionRequireApproval {
		t.Fatalf("first write = %q", out.Action)
	}

	// simulate approved create
	trust.SetPendingCreate("x.txt")
	if _, err := reg.Execute(context.Background(), "write_file", `{"path":"x.txt","content":"a","call_purpose":"t"}`); err != nil {
		t.Fatal(err)
	}
	exists, mtime, err := reg.StatRelPath("x.txt")
	if err != nil || !exists {
		t.Fatal(err)
	}
	_ = registryToolAfterEach(hooksReg, ToolAfterEachInput{
		ToolName:     "write_file",
		RawArguments: `{"path":"x.txt","content":"a","call_purpose":"t"}`,
		RawResult:    "ok",
	})

	// second edit: auto
	out = registryToolBeforeEach(hooksReg, ToolBeforeEachInput{
		ToolName: "search_replace",
		ToolArgs: map[string]any{"path": "x.txt", "old_string": "a", "new_string": "b"},
	})
	if out.Action != policy.ActionAuto {
		t.Fatalf("trusted edit = %q, mtime=%v owned=%v", out.Action, mtime, trust.IsOwned("x.txt"))
	}

	// external touch
	path := filepath.Join(root, "x.txt")
	if err := os.Chtimes(path, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	out = registryToolBeforeEach(hooksReg, ToolBeforeEachInput{
		ToolName: "write_file",
		ToolArgs: map[string]any{"path": "x.txt", "content": "c"},
	})
	if out.Action != policy.ActionRequireApproval {
		t.Fatalf("after external mtime change = %q", out.Action)
	}
}

func TestDuplicateHookSkipsWriteTools(t *testing.T) {
	log := &ToolExecutionLog{}
	log.RecordSuccess("write_file", ToolArgsFingerprint("write_file", `{"path":"a.txt","content":"x"}`), "prev", "ok")
	hook := NewDuplicateToolCallHook(DefaultDuplicateConfig())
	hook.SetLog(log)

	out := ToolBeforeEachResult{ToolMode: policy.ModeRule, Action: policy.ActionAuto}
	_ = hook.RunToolBeforeEach(context.Background(), ToolBeforeEachInput{
		ToolName:     "write_file",
		RawArguments: `{"path":"a.txt","content":"x"}`,
	}, &out)
	if out.Action != policy.ActionAuto {
		t.Fatalf("write_file should skip duplicate hook, got %q", out.Action)
	}
}
