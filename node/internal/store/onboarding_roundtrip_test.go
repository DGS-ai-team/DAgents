package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestOnboardingFalseRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := OpenNodeSettings(filepath.Join(dir, "node_settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seed := ProductNodeSettingsSeed()
	if seed.NodeProfileCompleted() {
		t.Fatal("seed should be incomplete")
	}
	if err := s.Save(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load(context.Background())
	if err != nil || loaded == nil {
		t.Fatalf("load: %v", err)
	}
	raw, _ := json.Marshal(loaded.Onboarding)
	t.Logf("onboarding json=%s", raw)
	if loaded.NodeProfileCompleted() {
		t.Fatal("loaded should still be incomplete")
	}
}
