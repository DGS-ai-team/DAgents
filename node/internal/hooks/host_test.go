package hooks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestWindowHistory(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "1"}, {Role: "user", Content: "2"}, {Role: "user", Content: "3"}}
	full := WindowHistory(msgs, 0)
	if len(full) != 3 {
		t.Fatalf("window 0: len = %d", len(full))
	}
	tail := WindowHistory(msgs, 2)
	if len(tail) != 2 || tail[0].Content != "2" {
		t.Fatalf("window 2: %+v", tail)
	}
}

func TestEnrichContextFromHost(t *testing.T) {
	host := &memoryHost{
		snap: HostSnapshot{
			History:      []llm.Message{{Role: "user", Content: "hi"}},
			SystemPrompt: "sys",
			LoadedSkills: []LoadedSkillInfo{{Name: "demo", Description: "d"}},
			Runtime:      RuntimeSummary{ToolLoopCount: 2},
			SessionStore: map[string]json.RawMessage{"k": json.RawMessage(`"v"`)},
		},
	}
	hc := &Context{SessionID: "s1"}
	EnrichContext(hc, host)
	if len(hc.History) != 1 || hc.SystemPrompt != "sys" {
		t.Fatalf("history/system = %+v / %q", hc.History, hc.SystemPrompt)
	}
	if hc.Runtime.ToolLoopCount != 2 {
		t.Fatalf("runtime = %+v", hc.Runtime)
	}
	if string(hc.SessionStore["k"]) != `"v"` {
		t.Fatalf("store = %v", hc.SessionStore)
	}
}

func TestSessionStoreMutation(t *testing.T) {
	hc := &Context{}
	if err := applyMutations(hc, map[string]any{
		MutationSessionStore: map[string]any{"count": 1},
	}); err != nil {
		t.Fatal(err)
	}
	if string(hc.SessionStore["count"]) != "1" {
		t.Fatalf("store = %v", hc.SessionStore)
	}
}

type memoryHost struct {
	snap HostSnapshot
}

func (h *memoryHost) Snapshot() HostSnapshot             { return h.snap }
func (h *memoryHost) SessionStoreGet(string) (any, bool) { return nil, false }
func (h *memoryHost) SessionStoreSet(string, any) error  { return nil }
func (h *memoryHost) SessionStoreDelete(string) error    { return nil }
func (h *memoryHost) LLMComplete(context.Context, LLMCompleteRequest) (LLMCompleteResponse, error) {
	return LLMCompleteResponse{}, ErrHostNotAvailable
}
