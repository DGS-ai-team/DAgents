package workgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDigestStableExcludesSelf(t *testing.T) {
	a := map[string]any{"b": 2, "a": map[string]any{"z": 1}, "digest": "sha256:dead"}
	b := map[string]any{"a": map[string]any{"z": 1}, "b": 2, "digest": "sha256:beef"}
	d1, err := SHA256Digest(a)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := SHA256Digest(b)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("digest mismatch %s vs %s", d1, d2)
	}
	if len(d1) != len("sha256:")+64 {
		t.Fatalf("digest len=%d", len(d1))
	}
}

func TestProvisionRetrySameIDOK(t *testing.T) {
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file", "bash"}})
	req := ProvisionRequest{
		ProvisionID:      "pv_01h00000000000000000000009",
		WorkgroupID:      "wg_01h00000000000000000000001",
		MemberID:         "mb_01h00000000000000000000002",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
		LeaseEpoch:       2,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	}
	r1, err := w.HandleProvision(req)
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Created || r1.Binding.Status != "ready" {
		t.Fatalf("first provision: %+v", r1)
	}
	if w.Provision.WorkspaceCreated != 1 {
		t.Fatalf("workspace_created=%d", w.Provision.WorkspaceCreated)
	}
	r2, err := w.HandleProvision(req)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Created {
		t.Fatal("retry should not recreate workspace")
	}
	if w.Provision.WorkspaceCreated != 1 {
		t.Fatalf("workspace_created_count want 1 got %d", w.Provision.WorkspaceCreated)
	}
	if r2.Binding.Status != "ready" {
		t.Fatalf("member_status=%s", r2.Binding.Status)
	}
}

func TestProvisionSameIDDifferentDigestConflict(t *testing.T) {
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	req := ProvisionRequest{
		ProvisionID:      "pv_01h00000000000000000000009",
		WorkgroupID:      "wg_01h00000000000000000000001",
		MemberID:         "mb_01h00000000000000000000002",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
		LeaseEpoch:       2,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	}
	if _, err := w.HandleProvision(req); err != nil {
		t.Fatal(err)
	}
	req.MemberSpecDigest = "sha256:d9298a10d1b0735837dc4bd85dac641b0f3cef27a47e5d53a54f2f3f5b2fcffa"
	_, err := w.HandleProvision(req)
	if err == nil {
		t.Fatal("expected conflict")
	}
	we, ok := err.(*Error)
	if !ok || we.Code != CodePayloadConflict {
		t.Fatalf("err=%v", err)
	}
}

func TestCommandNoReexecAfterAccept(t *testing.T) {
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	w.Connect()
	prov, err := w.HandleProvision(ProvisionRequest{
		ProvisionID:      "pv_01h00000000000000000000009",
		WorkgroupID:      "wg_01h00000000000000000000001",
		MemberID:         "mb_01h00000000000000000000002",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
		LeaseEpoch:       2,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	})
	if err != nil {
		t.Fatal(err)
	}
	execCount := 0
	w.Commands.Executor = func(cmd ToolCommand) (string, error) {
		execCount++
		return `{"ok":true}`, nil
	}
	cmd := ToolCommand{
		CommandID:           "cmd_01h00000000000000000000008",
		WorkgroupID:         prov.Binding.WorkgroupID,
		MemberID:            prov.Binding.MemberID,
		AssignID:            "as_01h00000000000000000000007",
		RunID:               "rn_01h00000000000000000000006",
		TurnID:              "tn_01h00000000000000000000005",
		ToolCallID:          "call_1",
		ToolName:            "read_file",
		ArgumentsJSON:       `{"path":"a.txt"}`,
		PayloadHash:         "sha256:f64551fcd6f07823cb87971cfb91446425da18286b3ab1ef935e0cbd7a69f68a",
		LeaseID:             "ls_01h00000000000000000000004",
		LeaseEpoch:          2,
		MemberGeneration:    1,
		MemberSpecDigest:    prov.Binding.MemberSpecDigest,
		ToolCatalogRevision: prov.Binding.ToolCatalogRevision,
		Status:              "queued",
		SideEffectClass:     "fs_read",
	}
	r1, err := w.HandleCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Executed || r1.Entry.Executions != 1 {
		t.Fatalf("first: %+v", r1)
	}
	r2, err := w.HandleCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Executed {
		t.Fatal("resend must not re-exec")
	}
	if execCount != 1 {
		t.Fatalf("executions=%d", execCount)
	}
	if r2.Entry.Status != "succeeded" {
		t.Fatalf("status=%s", r2.Entry.Status)
	}
}

func TestCatalogRevisionDrift(t *testing.T) {
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	w.Connect()
	prov, err := w.HandleProvision(ProvisionRequest{
		ProvisionID:      "pv_01h00000000000000000000009",
		WorkgroupID:      "wg_01h00000000000000000000001",
		MemberID:         "mb_01h00000000000000000000002",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
		LeaseEpoch:       2,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := ToolCommand{
		CommandID:           "cmd_01h00000000000000000000008",
		WorkgroupID:         prov.Binding.WorkgroupID,
		MemberID:            prov.Binding.MemberID,
		AssignID:            "as_01h00000000000000000000007",
		RunID:               "rn_01h00000000000000000000006",
		TurnID:              "tn_01h00000000000000000000005",
		ToolCallID:          "call_1",
		ToolName:            "read_file",
		ArgumentsJSON:       `{}`,
		PayloadHash:         "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		LeaseID:             "ls_01h00000000000000000000004",
		LeaseEpoch:          2,
		MemberGeneration:    1,
		MemberSpecDigest:    prov.Binding.MemberSpecDigest,
		ToolCatalogRevision: "rev_1",
		Status:              "queued",
		SideEffectClass:     "fs_read",
	}
	// Force node revision mismatch
	w.Commands.CatalogRevision = "rev_2"
	binding := prov.Binding
	binding.ToolCatalogRevision = "rev_2"
	_, err = w.Commands.Accept(cmd, binding)
	if err == nil {
		t.Fatal("expected catalog_drift")
	}
	we := err.(*Error)
	if we.Code != CodeCatalogDrift {
		t.Fatalf("code=%s", we.Code)
	}
}

func TestArchiveRejectsStaleEpoch(t *testing.T) {
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	w.Connect()
	prov, err := w.HandleProvision(ProvisionRequest{
		ProvisionID:      "pv_01h00000000000000000000009",
		WorkgroupID:      "wg_01h00000000000000000000001",
		MemberID:         "mb_01h00000000000000000000002",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
		LeaseEpoch:       3,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleArchive(ArchiveTombstone{
		WorkgroupID:         prov.Binding.WorkgroupID,
		LeaseEpochAtArchive: 4,
	}); err != nil {
		t.Fatal(err)
	}
	executed := false
	w.Commands.Executor = func(cmd ToolCommand) (string, error) {
		executed = true
		return `{}`, nil
	}
	_, err = w.HandleCommand(ToolCommand{
		CommandID:           "cmd_01h00000000000000000000008",
		WorkgroupID:         prov.Binding.WorkgroupID,
		MemberID:            prov.Binding.MemberID,
		AssignID:            "as_01h00000000000000000000007",
		RunID:               "rn_01h00000000000000000000006",
		TurnID:              "tn_01h00000000000000000000005",
		ToolCallID:          "call_1",
		ToolName:            "read_file",
		ArgumentsJSON:       `{}`,
		PayloadHash:         "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		LeaseID:             "ls_01h00000000000000000000004",
		LeaseEpoch:          3,
		MemberGeneration:    1,
		MemberSpecDigest:    prov.Binding.MemberSpecDigest,
		ToolCatalogRevision: prov.Binding.ToolCatalogRevision,
		Status:              "queued",
		SideEffectClass:     "fs_read",
	})
	if err == nil {
		t.Fatal("expected fencing_rejected")
	}
	we := err.(*Error)
	if we.Code != CodeFencingRejected && we.Code != CodeWorkgroupArchived {
		t.Fatalf("code=%s", we.Code)
	}
	if executed {
		t.Fatal("must not execute after archive")
	}
}

func TestEmptyAllowNamesMeansNoTools(t *testing.T) {
	names := EffectiveToolNames([]string{}, []string{"read_file"}, []string{"read_file", "bash"})
	if len(names) != 0 {
		t.Fatalf("effective=%v", names)
	}
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file", "bash"}})
	prov, err := w.HandleProvision(ProvisionRequest{
		ProvisionID:      "pv_01h00000000000000000000009",
		WorkgroupID:      "wg_01h00000000000000000000001",
		MemberID:         "mb_01h00000000000000000000002",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
		LeaseEpoch:       2,
		MemberGeneration: 1,
		ToolAllowNames:   nil,
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prov.Binding.ToolAllowNames) != 0 || len(prov.Manifest.Tools) != 0 {
		t.Fatalf("expected empty tools, binding=%v manifest=%v", prov.Binding.ToolAllowNames, prov.Manifest.Tools)
	}
}

func TestWorkerBindingNotLocalAgent(t *testing.T) {
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	prov, err := w.HandleProvision(ProvisionRequest{
		ProvisionID:      "pv_01h00000000000000000000009",
		WorkgroupID:      "wg_01h00000000000000000000001",
		MemberID:         "mb_01h00000000000000000000002",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
		LeaseEpoch:       2,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !prov.Binding.NotEnumerableAsLocalAgent {
		t.Fatal("must not be enumerable as local agent")
	}
	if w.IsLocalAgent(prov.Binding.MemberID) {
		t.Fatal("IsLocalAgent should be false")
	}
}

func TestSessionDupConnectionFencesOldGeneration(t *testing.T) {
	s := &Session{}
	g1 := s.Hello("node_b")
	g2 := s.Hello("node_b")
	if g2 <= g1 {
		t.Fatalf("generation should increase %d -> %d", g1, g2)
	}
	if err := s.FenceFrame(g1); err == nil {
		t.Fatal("old generation should be fenced")
	}
	if err := s.FenceFrame(g2); err != nil {
		t.Fatal(err)
	}
}

func TestReadFileExecutorHappyPath(t *testing.T) {
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	w.Connect()
	prov, err := w.HandleProvision(ProvisionRequest{
		ProvisionID:      "pv_01h00000000000000000000009",
		WorkgroupID:      "wg_01h00000000000000000000001",
		MemberID:         "mb_01h00000000000000000000002",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
		LeaseEpoch:       2,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prov.Binding.WorkspacePath, "README"), []byte("# Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := w.HandleCommand(ToolCommand{
		CommandID:           "cmd_01h00000000000000000000008",
		WorkgroupID:         prov.Binding.WorkgroupID,
		MemberID:            prov.Binding.MemberID,
		AssignID:            "as_01h00000000000000000000007",
		RunID:               "rn_01h00000000000000000000006",
		TurnID:              "tn_01h00000000000000000000005",
		ToolCallID:          "call_1",
		ToolName:            "read_file",
		ArgumentsJSON:       `{"path":"README"}`,
		PayloadHash:         "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		LeaseID:             "ls_01h00000000000000000000004",
		LeaseEpoch:          2,
		MemberGeneration:    1,
		MemberSpecDigest:    prov.Binding.MemberSpecDigest,
		ToolCatalogRevision: prov.Binding.ToolCatalogRevision,
		Status:              "queued",
		SideEffectClass:     "fs_read",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Executed || r.Entry.Status != "succeeded" {
		t.Fatalf("result=%+v", r)
	}
	if !strings.Contains(r.Entry.ResultJSON, "Demo") {
		t.Fatalf("content=%s", r.Entry.ResultJSON)
	}
}

func TestRecoverAcceptedBeforeRunning(t *testing.T) {
	dir := t.TempDir()
	journal, err := NewDirJournal(filepath.Join(dir, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(Config{
		NodeID:        "node_b",
		NodeToolNames: []string{"read_file"},
		Journal:       journal,
	})
	w.Connect()
	prov, err := w.HandleProvision(ProvisionRequest{
		ProvisionID:      "pv_01h00000000000000000000009",
		WorkgroupID:      "wg_01h00000000000000000000001",
		MemberID:         "mb_01h00000000000000000000002",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
		LeaseEpoch:       2,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prov.Binding.WorkspacePath, "README"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := ToolCommand{
		CommandID:           "cmd_01h00000000000000000000008",
		WorkgroupID:         prov.Binding.WorkgroupID,
		MemberID:            prov.Binding.MemberID,
		AssignID:            "as_01h00000000000000000000007",
		RunID:               "rn_01h00000000000000000000006",
		TurnID:              "tn_01h00000000000000000000005",
		ToolCallID:          "call_1",
		ToolName:            "read_file",
		ArgumentsJSON:       `{"path":"README"}`,
		PayloadHash:         "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		LeaseID:             "ls_01h00000000000000000000004",
		LeaseEpoch:          2,
		MemberGeneration:    1,
		MemberSpecDigest:    prov.Binding.MemberSpecDigest,
		ToolCatalogRevision: prov.Binding.ToolCatalogRevision,
		Status:              "queued",
		SideEffectClass:     "fs_read",
	}
	// 模拟：accept 后崩溃，journal 停留在 accepted
	now := "2026-07-31T00:00:00Z"
	if err := journal.Put(JournalEntry{
		CommandID:       cmd.CommandID,
		PayloadHash:     cmd.PayloadHash,
		Status:          "accepted",
		MemberID:        cmd.MemberID,
		WorkgroupID:     cmd.WorkgroupID,
		ToolName:        cmd.ToolName,
		SideEffectClass: cmd.SideEffectClass,
		Executions:      0,
		JournaledAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}
	execCount := 0
	w.Commands.Executor = func(c ToolCommand) (string, error) {
		execCount++
		return NewReadFileExecutor(w.Bindings)(c)
	}
	r1, err := w.HandleCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Executed || r1.Entry.Executions != 1 || r1.Entry.Status != "succeeded" {
		t.Fatalf("recover=%+v", r1)
	}
	r2, err := w.HandleCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Executed || execCount != 1 {
		t.Fatalf("resend must not reexec: exec=%d r2=%+v", execCount, r2)
	}
}
