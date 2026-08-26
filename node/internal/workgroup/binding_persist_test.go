package workgroup

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDirBindingStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDirBindingStore(filepath.Join(dir, "bindings"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	in := WorkerBinding{
		MemberID:                  "mb_persist_01",
		WorkgroupID:               "wg_persist_01",
		HomeNodeID:                "node_a",
		ProvisionID:               "pv_persist_01",
		MemberSpecDigest:          "sha256:abc",
		LeaseEpoch:                1,
		MemberGeneration:          1,
		WorkspacePath:             filepath.Join(dir, "ws"),
		Status:                    "ready",
		NotEnumerableAsLocalAgent: true,
		ToolAllowNames:            []string{"read_file", "glob_files"},
		ToolCatalogRevision:       "rev_1",
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}
	if err := store.Put(in); err != nil {
		t.Fatal(err)
	}

	// 新实例模拟 Node 重启
	store2, err := NewDirBindingStore(filepath.Join(dir, "bindings"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := store2.Get("mb_persist_01")
	if err != nil || got == nil {
		t.Fatalf("get: %v %+v", err, got)
	}
	if got.ProvisionID != in.ProvisionID || got.WorkspacePath != in.WorkspacePath {
		t.Fatalf("mismatch: %+v", got)
	}
	byPV, err := store2.GetByProvisionID("pv_persist_01")
	if err != nil || byPV == nil || byPV.MemberID != "mb_persist_01" {
		t.Fatalf("by provision: %v %+v", err, byPV)
	}
}

func TestNewWorkerDataDirPersistsBindings(t *testing.T) {
	dir := t.TempDir()
	w1 := NewWorker(Config{NodeID: "node_b", DataDir: dir, NodeToolNames: []string{"read_file"}})
	_, err := w1.HandleProvision(ProvisionRequest{
		ProvisionID:      "pv_01",
		WorkgroupID:      "wg_01",
		MemberID:         "mb_01",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:deadbeef",
		LeaseEpoch:       1,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    filepath.Join(dir, "ws"),
	})
	if err != nil {
		t.Fatal(err)
	}

	w2 := NewWorker(Config{NodeID: "node_b", DataDir: dir, NodeToolNames: []string{"read_file"}})
	b, err := w2.Bindings.Get("mb_01")
	if err != nil || b == nil {
		t.Fatalf("binding lost after restart: %v", err)
	}
}
