package compression

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestPrefixFullyClosed(t *testing.T) {
	t.Parallel()

	complete := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
		{Role: "tool", ToolCallID: "c1", Content: "ok"},
		{Role: "assistant", Content: "done"},
	}
	if !computePrefixClosure(complete).prefixClosed(len(complete) - 1) {
		t.Fatal("expected complete tool loop")
	}

	incompleteMissingTool := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
	}
	if computePrefixClosure(incompleteMissingTool).prefixClosed(len(incompleteMissingTool) - 1) {
		t.Fatal("expected incomplete when tool results missing")
	}

	incompleteExtraAssistant := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
		{Role: "assistant", Content: "next"},
	}
	if computePrefixClosure(incompleteExtraAssistant).prefixClosed(len(incompleteExtraAssistant) - 1) {
		t.Fatal("expected incomplete when tool_calls unanswered before next assistant")
	}

	multiTool := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "a", Type: "function", Function: llm.ToolCallFunction{Name: "t1"}},
			{ID: "b", Type: "function", Function: llm.ToolCallFunction{Name: "t2"}},
		}},
		{Role: "tool", ToolCallID: "b", Content: "b"},
		{Role: "tool", ToolCallID: "a", Content: "a"},
	}
	if !computePrefixClosure(multiTool).prefixClosed(len(multiTool) - 1) {
		t.Fatal("expected complete when all tool_call ids answered")
	}

	orphanTool := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "tool", ToolCallID: "c1", Content: "orphan"},
	}
	if computePrefixClosure(orphanTool).prefixClosed(len(orphanTool) - 1) {
		t.Fatal("expected incomplete for orphan tool message")
	}
}

func TestIsSelectableCompressEnd(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
	}
	closure := computePrefixClosure(msgs)
	if !closure.isSelectableCompressEnd(msgs, 0, "assistant") {
		t.Fatal("user before assistant is selectable for case 1")
	}
	if !closure.isSelectableCompressEnd(msgs, 1, "") {
		t.Fatal("text assistant is selectable")
	}
	if closure.isSelectableCompressEnd([]llm.Message{{Role: "user", Content: "only"}}, 0, "assistant") {
		t.Fatal("user without following assistant is not selectable")
	}
}

func TestBuildCompressionPlanKeepFollowingAssistant(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "tail"},
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected ok")
	}
	if plan.End != 2 || plan.ApplyMode != compressApplyKeepFollowingAssistant {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestBuildCompressionPlanAfterToolLoop(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "run"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "tc1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
		{Role: "tool", ToolCallID: "tc1", Content: "output"},
		{Role: "assistant", Content: "finished"},
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected ok")
	}
	if plan.End != 2 || plan.ApplyMode != compressApplyKeepFollowingAssistant {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestBuildCompressionPlanMergeNextUser(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next question"},
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected ok")
	}
	if plan.End != 1 || plan.ApplyMode != compressApplyMergeNextUser {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestBuildCompressionPlanMergeNextUserAfterToolHistory(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "run"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "tc1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
		{Role: "tool", ToolCallID: "tc1", Content: "output"},
		{Role: "assistant", Content: "finished"},
		{Role: "user", Content: "continue"},
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected ok")
	}
	if plan.End != 3 || plan.ApplyMode != compressApplyMergeNextUser {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestBuildCompressionPlanMultiToolOnlyLastToolIsBoundary(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "run"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "tc1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}},
			{ID: "tc2", Type: "function", Function: llm.ToolCallFunction{Name: "read_file"}},
		}},
		{Role: "tool", ToolCallID: "tc1", Content: "out1"},
		{Role: "tool", ToolCallID: "tc2", Content: "out2"},
		{Role: "assistant", Content: "done"},
	}
	closure := computePrefixClosure(msgs)
	if closure.isSelectableCompressEnd(msgs, 2, "assistant") {
		t.Fatal("prefix after first tool still has pending tc2")
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected ok")
	}
	if plan.End != 3 {
		t.Fatalf("must cut after last tool in batch, got end=%d plan=%+v", plan.End, plan)
	}
}

func TestBuildCompressionPlanPendingToolCallsOnly(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "run"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "tc1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected ok: user boundary before pending tool_calls assistant")
	}
	if plan.End != 0 || plan.ApplyMode != compressApplyKeepFollowingAssistant {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestBuildCompressionPlanInvalidSequenceNoCompress(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "run"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "tc1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
		{Role: "assistant", Content: "orphan text before tool result"},
		{Role: "user", Content: "next"},
	}
	if _, ok := buildCompressionPlan(msgs); ok {
		t.Fatal("expected false for invalid messages sequence")
	}
}

func TestBuildCompressionPlanNoAssistant(t *testing.T) {
	t.Parallel()

	if _, ok := buildCompressionPlan([]llm.Message{{Role: "user", Content: "only"}}); ok {
		t.Fatal("expected false without assistant")
	}
}

func TestBuildCompressionPlanSingleAssistantOnly(t *testing.T) {
	t.Parallel()

	if _, ok := buildCompressionPlan([]llm.Message{{Role: "assistant", Content: "only"}}); ok {
		t.Fatal("expected false when last assistant is first message")
	}
}

func TestBuildCompressionPlanCannotRollbackEnough(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "tc1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
		{Role: "assistant", Content: "tail"},
	}
	if _, ok := buildCompressionPlan(msgs); ok {
		t.Fatal("expected false when messages sequence is invalid")
	}
}

func TestEvaluateCompressionTriggerLevelWhenPlanFails(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "tc1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run"}}}},
		{Role: "assistant", Content: "tail"},
	}
	eval := evaluateCompression(msgs, 1, 0)
	if eval.Decision.Should {
		t.Fatalf("should not compress with invalid range, decision = %+v", eval.Decision)
	}
	if eval.Decision.TriggerLevel != "silent" {
		t.Fatalf("trigger = %q, want silent for threshold met but plan failed", eval.Decision.TriggerLevel)
	}
}

func TestEvaluateCompressionBelowThreshold(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{{Role: "user", Content: "hi"}}
	eval := evaluateCompression(msgs, 1000, 2000)
	if eval.Decision.Should || eval.Decision.TriggerLevel != "none" {
		t.Fatalf("decision = %+v", eval.Decision)
	}
}

func TestLeadingSystemSkipPreservesPrefixOnApply(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "system", Content: "journal-injected"},
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "tail"},
	}
	if leadingSystemSkip(msgs) != 1 {
		t.Fatalf("skip = %d", leadingSystemSkip(msgs))
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected plan")
	}
	fp := compressionSourceFingerprint(msgs, plan)
	merged, status := applyCompressionReplacement(msgs, plan, "summary", fp)
	if status != "applied" {
		t.Fatalf("status = %q", status)
	}
	if len(merged) != 3 {
		t.Fatalf("len = %d, msgs = %+v", len(merged), merged)
	}
	if merged[0].Role != "system" || merged[0].Content != "journal-injected" {
		t.Fatalf("leading system = %+v", merged[0])
	}
	if merged[1].Content != "summary" || merged[2].Content != "tail" {
		t.Fatalf("tail = %+v", merged[1:])
	}
}

func TestBuildSidecarSkipsLeadingSystem(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "system", Content: "dup"},
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
	}
	req := BuildSidecarChatRequest(SidecarInput{
		SidecarPrefix: SidecarPrefix{SystemPrompt: "real-system", Tools: sampleSidecarTools()},
		Messages:      msgs,
		End:           1,
		SidecarAppend: sidecarAppendUserOnly,
	}, summaryUserPrompt)
	if len(req.Messages) != 2 { // user a + summary user（跳过 leading system）
		t.Fatalf("len = %d", len(req.Messages))
	}
	if req.Messages[0].Role != "user" || req.Messages[0].Content != "a" {
		t.Fatalf("first msg = %+v", req.Messages[0])
	}
}

func TestIdleContextBoundaryIsPreservedAsOnlyRuntimeLeadingPrefix(t *testing.T) {
	t.Parallel()
	boundary := llm.UserMessageWithSource(
		"bridge body", llm.UserNameConversationBoundary,
		llm.MessageSource{Kind: llm.MessageSourceRuntime, Form: llm.MessageFormNotice},
		&llm.MessageProvenance{Producer: "idle_context", Operation: "continue-from-archive", Reference: "/workspace/.dagents/a/history/day/s-r.json"},
	)
	skill := llm.UserMessage("skill instructions", llm.UserNameSkill)
	otherRuntime := llm.UserMessageWithSource("keep normally compressible", "runtime", llm.MessageSource{Kind: llm.MessageSourceRuntime, Form: llm.MessageFormNotice}, &llm.MessageProvenance{Producer: "other_runtime"})
	msgs := []llm.Message{
		{Role: "system", Content: "journal system"},
		boundary,
		skill,
		otherRuntime,
		{Role: "user", Content: "previous task"},
		{Role: "assistant", Content: "previous answer"},
		{Role: "user", Content: "current task"},
		{Role: "assistant", Content: "tail"},
	}
	if got := leadingSystemSkip(msgs); got != 2 {
		t.Fatalf("leading protected prefix = %d, want system + boundary", got)
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected compression plan")
	}
	if got := compressionSlice(msgs, plan); len(got) == 0 || got[0].Name != llm.UserNameSkill {
		t.Fatalf("compression slice should begin after bridge: %+v", got)
	}
	fp := compressionSourceFingerprint(msgs, plan)
	merged, status := applyCompressionReplacement(msgs, plan, "summary", fp)
	if status != "applied" {
		t.Fatalf("apply status = %q", status)
	}
	if len(merged) < 3 || merged[0].Content != "journal system" || !llm.IsMessageSource(merged[1], llm.MessageSourceRuntime, llm.MessageFormNotice, "idle_context") || merged[1].Content != boundary.Content {
		t.Fatalf("boundary prefix was not preserved: %+v", merged)
	}
	if merged[2].Name != llm.UserNameCompression {
		t.Fatalf("expected other leading content to be compressible, got %+v", merged[2])
	}
	if !llm.IsHiddenInjectedUserMessage(merged[1]) {
		t.Fatal("boundary should remain hidden from hydrate transcript")
	}
}

func TestBuildSidecarSkipsSystemAndBoundaryPrefix(t *testing.T) {
	t.Parallel()
	boundary := llm.UserMessageWithSource("bridge", llm.UserNameConversationBoundary, llm.MessageSource{Kind: llm.MessageSourceRuntime, Form: llm.MessageFormNotice}, &llm.MessageProvenance{Producer: "idle_context"})
	msgs := []llm.Message{{Role: "system", Content: "dup"}, boundary, llm.UserMessage("task", llm.UserNameHuman), {Role: "assistant", Content: "done"}}
	req := BuildSidecarChatRequest(SidecarInput{SidecarPrefix: SidecarPrefix{SystemPrompt: "real"}, Messages: msgs, End: 2, SidecarAppend: sidecarAppendUserOnly}, "summarize")
	for _, m := range req.Messages {
		if llm.IsMessageSource(m, llm.MessageSourceRuntime, llm.MessageFormNotice, "idle_context") {
			t.Fatal("bridge should be excluded from summary source, while preserved in main history")
		}
	}
	if len(req.Messages) != 2 || req.Messages[0].Content != "task" {
		t.Fatalf("sidecar messages = %+v", req.Messages)
	}
}

func TestMergeSummaryWithUser(t *testing.T) {
	t.Parallel()

	got := mergeSummaryWithUser("summary", "question")
	if got != "summary\n\nquestion" {
		t.Fatalf("got %q", got)
	}
}
