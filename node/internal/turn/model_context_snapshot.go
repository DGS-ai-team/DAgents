package turn

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// ModelContextSnapshot freezes the model-visible non-history inputs for one
// context segment. History is still appended normally; explicit context
// mutations may replace the segment at the next model Step, never in place
// during an active model request. Compression also invalidates this segment
// before the next request so compacted history is not paired with stale inputs.
type ModelContextSnapshot struct {
	SystemPrompt           string
	ToolDefinitions        []tools.ToolDef
	ContextInjections      []ContextInjection
	ContextInjectionDigest string
	RuntimeRevision        int64
	RuntimeDigest          string
	PromptDigest           string
	ToolDigest             string
	SkillsCatalogRevision  string
	LoadedSkillsDigest     string
	// LoadedSkillsContentDigest distinguishes the actual loaded SKILL.md
	// bodies from the loaded-name set. It is diagnostic metadata only; the
	// durable skill context messages in history remain the model-facing source.
	LoadedSkillsContentDigest string
	MemorySnapshotID          string
	MemoryStoreRevision       int64
	MemoryDigest              string
	MemoryCoreCount           int
	MemoryRecallCount         int
	MemoryEstimatedTokens     int
}

func (s *ModelContextSnapshot) observability() map[string]any {
	if s == nil {
		return nil
	}
	return map[string]any{
		"runtime_revision":             s.RuntimeRevision,
		"runtime_generation":           s.RuntimeRevision,
		"context_revision":             s.RuntimeRevision,
		"runtime_digest":               s.RuntimeDigest,
		"prompt_digest":                s.PromptDigest,
		"tool_digest":                  s.ToolDigest,
		"context_injection_digest":     s.ContextInjectionDigest,
		"context_injection_count":      len(s.ContextInjections),
		"skills_catalog_revision":      s.SkillsCatalogRevision,
		"loaded_skills_digest":         s.LoadedSkillsDigest,
		"loaded_skills_content_digest": s.LoadedSkillsContentDigest,
		"memory_snapshot_id":           s.MemorySnapshotID,
		"memory_store_revision":        s.MemoryStoreRevision,
		"memory_digest":                s.MemoryDigest,
		"memory_core_count":            s.MemoryCoreCount,
		"memory_recall_count":          s.MemoryRecallCount,
		"memory_estimated_tokens":      s.MemoryEstimatedTokens,
	}
}

// NewModelContextSnapshot creates a defensive copy of the tool definitions.
func NewModelContextSnapshot(systemPrompt string, defs []tools.ToolDef, revision int64, runtimeDigest string) *ModelContextSnapshot {
	return NewModelContextSnapshotWithInjections(systemPrompt, defs, nil, revision, runtimeDigest)
}

// NewModelContextSnapshotWithInjections freezes the complete model-visible
// request inputs for a Turn/Step segment. Context injections are cloned and
// retained in the snapshot so a retry or lifecycle restore uses the exact
// same request context.
func NewModelContextSnapshotWithInjections(systemPrompt string, defs []tools.ToolDef, injections []ContextInjection, revision int64, runtimeDigest string) *ModelContextSnapshot {
	copyDefs := cloneToolDefinitions(defs)
	copyInjections := cloneContextInjections(injections)
	injectionDigest := ""
	if len(copyInjections) > 0 {
		injectionDigest = Digest(copyInjections)
	}
	if strings.TrimSpace(runtimeDigest) == "" {
		runtimeDigest = RuntimeDigestFromInputs(nil, systemPrompt, copyDefs)
	}
	return &ModelContextSnapshot{
		SystemPrompt:           systemPrompt,
		ToolDefinitions:        copyDefs,
		ContextInjections:      copyInjections,
		ContextInjectionDigest: injectionDigest,
		RuntimeRevision:        revision,
		RuntimeDigest:          strings.TrimSpace(runtimeDigest),
		PromptDigest:           Digest(systemPrompt),
		ToolDigest:             Digest(copyDefs),
	}
}

// Clone returns a defensive copy suitable for callers and test assertions.
func (s *ModelContextSnapshot) Clone() *ModelContextSnapshot {
	if s == nil {
		return nil
	}
	copy := *s
	copy.ToolDefinitions = cloneToolDefinitions(s.ToolDefinitions)
	copy.ContextInjections = cloneContextInjections(s.ContextInjections)
	return &copy
}

// Digest returns a stable SHA-256 digest of a JSON-serializable value. JSON
// encoding sorts map keys, so equivalent tool schemas do not drift because of
// Go map iteration order.
func Digest(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// RuntimeDigestFromInputs identifies the model-visible runtime inputs without
// putting diagnostics into the system prompt itself.
func RuntimeDigestFromInputs(snapshot any, systemPrompt string, defs []tools.ToolDef) string {
	return Digest(struct {
		Snapshot     any             `json:"snapshot"`
		SystemPrompt string          `json:"system_prompt"`
		Tools        []tools.ToolDef `json:"tools"`
	}{snapshot, systemPrompt, cloneToolDefinitions(defs)})
}

func cloneToolDefinitions(defs []tools.ToolDef) []tools.ToolDef {
	if len(defs) == 0 {
		return nil
	}
	raw, err := json.Marshal(defs)
	if err != nil {
		return append([]tools.ToolDef(nil), defs...)
	}
	var copyDefs []tools.ToolDef
	if err := json.Unmarshal(raw, &copyDefs); err != nil {
		return append([]tools.ToolDef(nil), defs...)
	}
	return copyDefs
}

type modelContextSnapshotStore struct {
	mu   sync.RWMutex
	data map[string]*ModelContextSnapshot
}

func newModelContextSnapshotStore() *modelContextSnapshotStore {
	return &modelContextSnapshotStore{data: make(map[string]*ModelContextSnapshot)}
}

func (s *modelContextSnapshotStore) get(sessionID string) *ModelContextSnapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[sessionID].Clone()
}

func (s *modelContextSnapshotStore) set(sessionID string, snapshot *ModelContextSnapshot) {
	if s == nil || strings.TrimSpace(sessionID) == "" || snapshot == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[sessionID] = snapshot.Clone()
}

func (s *modelContextSnapshotStore) clear(sessionID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, sessionID)
}
