package turn

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// ModelContextSnapshot freezes the model-visible runtime inputs for one full
// Turn. History is still appended normally; the system prompt and tool schema
// do not change between the human step and later tool-result steps.
type ModelContextSnapshot struct {
	SystemPrompt    string
	ToolDefinitions []tools.ToolDef
	RuntimeRevision int64
	RuntimeDigest   string
	PromptDigest    string
	ToolDigest      string
}

func (s *ModelContextSnapshot) observability() map[string]any {
	if s == nil {
		return nil
	}
	return map[string]any{
		"runtime_revision":   s.RuntimeRevision,
		"runtime_generation": s.RuntimeRevision,
		"context_revision":   s.RuntimeRevision,
		"runtime_digest":     s.RuntimeDigest,
		"prompt_digest":      s.PromptDigest,
		"tool_digest":        s.ToolDigest,
	}
}

// NewModelContextSnapshot creates a defensive copy of the tool definitions.
func NewModelContextSnapshot(systemPrompt string, defs []tools.ToolDef, revision int64, runtimeDigest string) *ModelContextSnapshot {
	copyDefs := cloneToolDefinitions(defs)
	if strings.TrimSpace(runtimeDigest) == "" {
		runtimeDigest = RuntimeDigestFromInputs(nil, systemPrompt, copyDefs)
	}
	return &ModelContextSnapshot{
		SystemPrompt:    systemPrompt,
		ToolDefinitions: copyDefs,
		RuntimeRevision: revision,
		RuntimeDigest:   strings.TrimSpace(runtimeDigest),
		PromptDigest:    Digest(systemPrompt),
		ToolDigest:      Digest(copyDefs),
	}
}

// Clone returns a defensive copy suitable for callers and test assertions.
func (s *ModelContextSnapshot) Clone() *ModelContextSnapshot {
	if s == nil {
		return nil
	}
	copy := *s
	copy.ToolDefinitions = cloneToolDefinitions(s.ToolDefinitions)
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
