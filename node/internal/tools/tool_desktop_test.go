package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/computeruse"
	"github.com/DGS-ai-team/DAgents/node/internal/screen"
)

func TestDesktopToolDefinitions(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, def := range reg.Definitions() {
		names[def.Function.Name] = true
	}
	if !names["screen_capture"] || !names["computer_use"] {
		t.Fatalf("desktop tool definitions missing: %v", names)
	}
}

func TestComputerUseRequiresMultimodalModel(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reg.execComputerUse(context.Background(), []byte(`{"action":"key","key":"escape"}`))
	if err == nil || !strings.Contains(err.Error(), "requires a multimodal model") {
		t.Fatalf("err = %v, want multimodal requirement", err)
	}
	if !strings.Contains(result, `"status":"failed"`) {
		t.Fatalf("result = %s, want structured failure", result)
	}
}

func TestComputerUseDefinitionSupportsBoundedBatches(t *testing.T) {
	params := computerUseToolDef().Function.Parameters
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", params["properties"])
	}
	batch, ok := properties["actions"].(map[string]any)
	if !ok {
		t.Fatalf("actions schema = %#v", properties["actions"])
	}
	if got := batch["maxItems"]; got != maxComputerUseBatchActions {
		t.Fatalf("actions maxItems = %#v, want %d", got, maxComputerUseBatchActions)
	}
	if _, ok := properties["frame_id"]; !ok {
		t.Fatal("frame_id is missing from computer_use schema")
	}
	if _, ok := params["anyOf"]; ok {
		t.Fatal("computer_use schema should keep the action/actions choice provider-neutral")
	}
}

func TestValidateComputerUseBatchAllowsSafeFollowUpKeysOnly(t *testing.T) {
	if err := validateComputerUseBatch([]computeruse.Action{
		{Name: computeruse.ActionClick, HasPoint: true},
		{Name: computeruse.ActionTypeText, Text: "hello"},
		{Name: computeruse.ActionKey, Key: "enter"},
	}); err != nil {
		t.Fatalf("safe batch rejected: %v", err)
	}
	if err := validateComputerUseBatch([]computeruse.Action{
		{Name: computeruse.ActionClick, HasPoint: true},
		{Name: computeruse.ActionScroll, ScrollY: 2},
	}); err == nil {
		t.Fatal("expected two coordinate-dependent actions to be rejected")
	}
}

func TestComputerUseRejectsMismatchedFrameID(t *testing.T) {
	x, y := 10, 10
	reg := &Registry{desktopFrames: map[string]screenGeometry{
		"desktop-session": {
			Width:      100,
			Height:     100,
			Bounds:     screen.Bounds{Width: 100, Height: 100},
			CapturedAt: time.Now(),
			FrameID:    "frame-current",
		},
	}}
	ctx := WithSession(context.Background(), "desktop-session")
	_, err := reg.resolveComputerAction(ctx, computerUseArgs{
		Action:  computeruse.ActionClick,
		X:       &x,
		Y:       &y,
		FrameID: "frame-old",
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v, want frame mismatch", err)
	}
}

func TestExecScreenCaptureUsesRealDisplay(t *testing.T) {
	if status := screen.DetectStatus(); !status.Available {
		t.Skipf("display unavailable: %s", status.Reason)
	}
	reg, err := NewRegistry(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithToolCallID(WithSession(context.Background(), "desktop-test"), "capture-1")
	result, err := reg.execScreenCapture(ctx, []byte(`{"detail":"low"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK         bool   `json:"ok"`
		Screenshot string `json:"screenshot_path"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode result: %v; result=%s", err, result)
	}
	if !payload.OK || payload.Screenshot == "" {
		t.Fatalf("unexpected result: %s", result)
	}
	t.Cleanup(func() { _ = os.Remove(payload.Screenshot) })
	info, err := os.Stat(payload.Screenshot)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 32 {
		t.Fatalf("screenshot too small: %d bytes", info.Size())
	}
}

func TestMapScreenPointHandlesScaledVirtualDesktop(t *testing.T) {
	geometry := screenGeometry{
		Width:      1000,
		Height:     500,
		Bounds:     screen.Bounds{X: -1920, Y: 0, Width: 3840, Height: 1080},
		CapturedAt: time.Now(),
	}
	x, y, err := mapScreenPoint(geometry, 500, 250)
	if err != nil {
		t.Fatal(err)
	}
	if x != 0 || y != 540 {
		t.Fatalf("mapped point = (%d,%d), want (0,540)", x, y)
	}
	if _, _, err := mapScreenPoint(geometry, 1000, 250); err == nil {
		t.Fatal("expected out-of-bounds error")
	}
}

func TestDesktopStateAndFilesArePruned(t *testing.T) {
	now := time.Now()
	frames := map[string]screenGeometry{
		"old":   {CapturedAt: now.Add(-11 * time.Minute)},
		"fresh": {CapturedAt: now.Add(-time.Minute)},
	}
	pruneDesktopFrames(frames, now.Add(-10*time.Minute))
	if _, ok := frames["old"]; ok {
		t.Fatal("stale desktop geometry was not pruned")
	}
	if _, ok := frames["fresh"]; !ok {
		t.Fatal("fresh desktop geometry was pruned")
	}

	root := t.TempDir()
	oldDir := filepath.Join(root, "old-session")
	newDir := filepath.Join(root, "new-session")
	if err := os.MkdirAll(oldDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(oldDir, "old.jpg")
	newFile := filepath.Join(newDir, "new.jpg")
	if err := os.WriteFile(oldFile, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldFile, now.Add(-25*time.Hour), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	cleanupOldDesktopFrameTree(root, now.Add(-24*time.Hour))
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("old screenshot still exists: %v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("fresh screenshot missing: %v", err)
	}
}
