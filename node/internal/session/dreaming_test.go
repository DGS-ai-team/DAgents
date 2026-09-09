package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func dreamingFixture(t *testing.T, client llm.Client) (*Manager, *tools.Registry, string, func()) {
	t.Helper()
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{
		"read_file": policy.ModeNever, "write_file": policy.ModeNever,
		"search_replace": policy.ModeNever, "glob_files": policy.ModeNever,
		"grep_file": policy.ModeNever, "grep_files": policy.ModeNever,
	}})
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, allow, nil, TurnOptions{AutoAgent: true}, logx.Discard())
	rt, _, err := mgr.CreateWithOptionsAndLLM("dreaming", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		mgr.Stop()
		t.Fatal(err)
	}
	return mgr, reg, rt.ID, func() { mgr.Stop() }
}

func TestRunDreamingReadOnlyReturnsContentWithoutChange(t *testing.T) {
	mgr, reg, sessionID, cleanup := dreamingFixture(t, &handbookRoundClient{})
	defer cleanup()
	ctx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	defer release()
	result, err := mgr.RunDreaming(ctx, sessionID, "整理经验", 4)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" || result.Changed || !result.UsageKnown {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(reg.HandbookRoot(), "guide.md")); !os.IsNotExist(err) {
		t.Fatalf("read-only dreaming changed handbook: %v", err)
	}
}

func TestRunDreamingUsesHandbookToolsAndReturnsFinalExperience(t *testing.T) {
	mgr, reg, sessionID, cleanup := dreamingFixture(t, &handbookEditClient{})
	defer cleanup()
	ctx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	defer release()
	result, err := mgr.RunDreaming(ctx, sessionID, "整理并总结经验", 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" || !result.Changed {
		t.Fatalf("unexpected result: %+v", result)
	}
	got, err := os.ReadFile(filepath.Join(reg.HandbookRoot(), "a", "b", "c.md"))
	if err != nil || string(got) != "new" {
		t.Fatalf("handbook=%q err=%v", got, err)
	}
}

func TestRunDreamingMaxToolRoundsLeavesFinalRequestToolFree(t *testing.T) {
	client := &handbookRoundClient{}
	mgr, _, sessionID, cleanup := dreamingFixture(t, client)
	defer cleanup()
	ctx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	defer release()
	result, err := mgr.RunDreaming(ctx, sessionID, "整理经验", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content == "" || client.calls != 2 {
		t.Fatalf("max tool rounds did not reserve a tool-free final request: result=%+v calls=%d", result, client.calls)
	}
}

func TestRunDreamingIgnoresChatBudgetAndRestoresIt(t *testing.T) {
	mgr, _, sessionID, cleanup := dreamingFixture(t, &handbookRoundClient{})
	defer cleanup()
	mgr.mu.RLock()
	r := mgr.sessions[sessionID]
	mgr.mu.RUnlock()
	if r == nil {
		t.Fatal("runtime missing")
	}
	r.lifecycleMu.Lock()
	r.budgetResolver = func() (turn.TurnBudget, error) {
		return turn.TurnBudget{MaxTotalTokens: 1, MaxToolRounds: 99}, nil
	}
	r.lifecycleMu.Unlock()
	ctx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	defer release()
	if _, err := mgr.RunDreaming(ctx, sessionID, "整理经验", 2); err != nil {
		t.Fatal(err)
	}
	r.lifecycleMu.Lock()
	budget, resolveErr := r.budgetResolver()
	restored := r.turnBudget
	r.lifecycleMu.Unlock()
	if resolveErr != nil || budget.MaxTotalTokens != 1 || budget.MaxToolRounds != 99 {
		t.Fatalf("chat budget resolver was not restored: budget=%+v err=%v", budget, resolveErr)
	}
	if restored != (turn.TurnBudget{}) {
		t.Fatalf("dreaming budget leaked into later chat: %+v", restored)
	}
}

func TestRunDreamingHonorsHandbookApproval(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	client := &handbookEditClient{}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{
		"write_file": policy.ModeAlways,
	}})
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, allow, nil, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("dreaming-approval", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	rBefore := func() int { return len(mgr.sessions[rt.ID].activeMessagesSnapshot()) }
	before := rBefore()
	ctx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	defer release()
	result, runErr := mgr.RunDreaming(ctx, rt.ID, "整理经验", 2)
	if runErr == nil || result.Content != "" {
		t.Fatalf("approval was bypassed: result=%+v err=%v", result, runErr)
	}
	if _, err := os.Stat(filepath.Join(handbook, "a", "b", "c.md")); !os.IsNotExist(err) {
		t.Fatalf("approval-blocked dreaming wrote handbook: %v", err)
	}
	if after := rBefore(); after <= before {
		t.Fatalf("approval-blocked turn unexpectedly cleared active context: before=%d after=%d", before, after)
	}
}

func TestRunDreamingDoesNotRequireKnownUsage(t *testing.T) {
	client := &handbookEditClient{noUsageSecond: true}
	mgr, _, sessionID, cleanup := dreamingFixture(t, client)
	defer cleanup()
	ctx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	defer release()
	result, err := mgr.RunDreaming(ctx, sessionID, "整理经验", 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" || !result.Changed {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.UsageKnown {
		t.Fatalf("dreaming unexpectedly required/obtained complete usage: %+v", result)
	}
}

func TestRunDreamingCancellationDoesNotReportSuccess(t *testing.T) {
	client := &handbookBlockingClient{started: make(chan struct{})}
	mgr, reg, sessionID, cleanup := dreamingFixture(t, client)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(ctx, "agent-1")
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	done := make(chan error, 1)
	go func() {
		_, runErr := mgr.RunDreaming(leaseCtx, sessionID, "整理经验", 2)
		done <- runErr
	}()
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("model did not start")
	}
	cancel()
	release()
	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("cancelled dreaming reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled dreaming did not stop")
	}
	if _, err := os.Stat(filepath.Join(reg.HandbookRoot(), "written.md")); !os.IsNotExist(err) {
		t.Fatalf("cancelled dreaming wrote handbook: %v", err)
	}
}
