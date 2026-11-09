package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserMessageMaterializesStructuredSource(t *testing.T) {
	m := UserMessage("write it", UserNameSkill)
	if m.Source == nil || m.Source.Kind != MessageSourcePlugin || m.Source.Form != MessageFormInstructions {
		t.Fatalf("source = %#v", m.Source)
	}
	if m.Provenance == nil || m.Provenance.Producer != "skills" {
		t.Fatalf("provenance = %#v", m.Provenance)
	}
}

func TestMessageSourceSurvivesJSONRoundTrip(t *testing.T) {
	original := UserMessageWithSource(
		"body",
		UserNameSkill,
		MessageSource{Kind: MessageSourcePlugin, Form: MessageFormInstructions},
		&MessageProvenance{Producer: "skills", Operation: "load", Reference: "writer"},
	)
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Source == nil || restored.Source.Kind != MessageSourcePlugin || restored.Provenance == nil || restored.Provenance.Reference != "writer" {
		t.Fatalf("restored = %#v", restored)
	}
}

func TestTurnCancellationMarkerIsDurableButSentAsLeadingSystemInstruction(t *testing.T) {
	cancelled := UserMessageWithSource("write the full training manual", UserNameHuman, MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, &MessageProvenance{Producer: "human", Reference: "turn-cancelled"})
	marker := TurnCancellationContextMessage("turn-cancelled")
	followup := UserMessage("only answer the latest short question", UserNameHuman)
	if !IsTurnCancellationContextMessage(marker) {
		t.Fatalf("cancellation marker not recognized: %#v", marker)
	}
	encodedMarker, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	var restoredMarker Message
	if err := json.Unmarshal(encodedMarker, &restoredMarker); err != nil {
		t.Fatal(err)
	}
	if !IsTurnCancellationContextMessage(restoredMarker) || restoredMarker.Provenance.Reference != "turn-cancelled" {
		t.Fatalf("cancellation marker did not survive history serialization: %#v", restoredMarker)
	}

	wire := MessagesWithSystem("base instructions", []Message{cancelled, marker, followup})
	if len(wire) != 3 {
		t.Fatalf("wire messages = %#v, want leading system plus two user messages", wire)
	}
	if wire[0].Role != "system" || !strings.Contains(wire[0].Content, TurnIncompleteSystemInstruction) {
		t.Fatalf("leading system message does not carry cancellation boundary: %#v", wire[0])
	}
	if wire[1].Role != "user" || !strings.Contains(wire[1].Content, "此用户请求在完成前已取消") || !strings.HasSuffix(wire[1].Content, cancelled.Content) || wire[2].Role != "user" || wire[2].Content != followup.Content {
		t.Fatalf("user history order changed: %#v", wire)
	}
	for _, message := range wire {
		if IsTurnCancellationContextMessage(message) {
			t.Fatal("internal cancellation marker leaked as an in-line provider message")
		}
	}
	if err := ValidateToolProtocol(wire); err != nil {
		t.Fatalf("cancellation-boundary payload is invalid: %v", err)
	}
}

func TestCancellationProjectionMarksOnlyCancelledRequestAndAllowsExplicitResume(t *testing.T) {
	completedA := UserMessageWithSource("完成的A", UserNameHuman, MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, &MessageProvenance{Producer: "human", Reference: "turn-a"})
	completedReply := Message{Role: "assistant", Content: "A已经完成"}
	cancelledB := UserMessageWithSource("撰写B", UserNameHuman, MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, &MessageProvenance{Producer: "human", Reference: "turn-b"})
	cancellationB := TurnCancellationContextMessage("turn-b")
	latestC := UserMessageWithSource("只处理C", UserNameHuman, MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, &MessageProvenance{Producer: "human", Reference: "turn-c"})
	history := []Message{completedA, completedReply, cancelledB, cancellationB, latestC}

	wire := MessagesWithSystem("base", history)
	if len(wire) != 5 || wire[1].Content != "完成的A" || wire[2].Content != "A已经完成" || wire[3].Content != turnCancellationUserPrefix+"撰写B" || wire[4].Content != "只处理C" {
		t.Fatalf("A→cancel B→C projection = %#v", wire)
	}
	if !strings.Contains(wire[0].Content, TurnIncompleteSystemInstruction) {
		t.Fatalf("unresolved cancellation instruction missing: %#v", wire[0])
	}

	repeated := append(append([]Message(nil), history...), TurnCancellationContextMessage("turn-c"))
	repeatedWire := MessagesWithSystem("base", repeated)
	if len(repeatedWire) != 5 || repeatedWire[3].Content != turnCancellationUserPrefix+"撰写B" || repeatedWire[4].Content != turnCancellationUserPrefix+"只处理C" || !strings.Contains(repeatedWire[0].Content, TurnIncompleteSystemInstruction) {
		t.Fatalf("repeated cancellation projection = %#v", repeatedWire)
	}

	continued := append(append([]Message(nil), repeated...), UserMessage("明确继续B，请恢复刚才的撰写任务", UserNameHuman))
	continuedWire := MessagesWithSystem("base", continued)
	if !strings.Contains(continuedWire[0].Content, "只有当最新用户消息明确要求恢复") || !strings.HasSuffix(continuedWire[len(continuedWire)-1].Content, "明确继续B，请恢复刚才的撰写任务") {
		t.Fatalf("explicit resume context = %#v", continuedWire)
	}

	completedC := append(append([]Message(nil), history...), Message{Role: "assistant", Content: "已处理C"}, UserMessage("新问题D", UserNameHuman))
	completedWire := MessagesWithSystem("base", completedC)
	if strings.Contains(completedWire[0].Content, TurnIncompleteSystemInstruction) {
		t.Fatalf("resolved cancellation should not keep adding global instruction: %#v", completedWire[0])
	}
	if !strings.HasPrefix(completedWire[3].Content, turnCancellationUserPrefix) || completedWire[len(completedWire)-1].Content != "新问题D" {
		t.Fatalf("durable per-request marker lost after later completion: %#v", completedWire)
	}
	emptyPromptWire := MessagesWithSystem("", completedC)
	for _, message := range emptyPromptWire {
		if IsTurnCancellationContextMessage(message) {
			t.Fatalf("resolved marker leaked with empty system prompt: %#v", emptyPromptWire)
		}
	}
}

func TestCancellationMarkerWithMissingTurnDoesNotMarkPreviousUser(t *testing.T) {
	completedA := UserMessageWithSource("完成的A", UserNameHuman, MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, &MessageProvenance{Producer: "human", Reference: "turn-a"})
	markerForNotYetAppendedB := TurnCancellationContextMessage("turn-b")
	wire := MessagesWithSystem("", []Message{completedA, markerForNotYetAppendedB})
	if len(wire) != 2 || wire[0].Role != "system" || !strings.Contains(wire[0].Content, TurnIncompleteSystemInstruction) || wire[1].Content != completedA.Content {
		t.Fatalf("missing turn marker must not mark prior user: %#v", wire)
	}
	for _, message := range wire {
		if IsTurnCancellationContextMessage(message) {
			t.Fatalf("internal cancellation marker leaked into provider history: %#v", wire)
		}
	}
	withSystem := MessagesWithSystem("base", []Message{completedA, markerForNotYetAppendedB})
	if len(withSystem) != 2 || !strings.Contains(withSystem[0].Content, TurnIncompleteSystemInstruction) || withSystem[1].Content != completedA.Content {
		t.Fatalf("missing turn marker should retain global cancellation instruction without marking prior user: %#v", withSystem)
	}
}

func TestFailedTurnProjectionPrioritizesLatestIntentAndPreservesToolPairing(t *testing.T) {
	completedA := UserMessageWithSource("完成的A", UserNameHuman, MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, &MessageProvenance{Producer: "human", Reference: "turn-a"})
	completedReply := Message{Role: "assistant", Content: "A已经完成"}
	failedB := UserMessageWithSource("只回答配额分类探针", UserNameHuman, MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, &MessageProvenance{Producer: "human", Reference: "turn-b"})
	call := ToolCall{ID: "call-before-failure", Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"existing.txt"}`}}
	toolAssistant := Message{Role: "assistant", ToolCalls: []ToolCall{call}}
	toolResult := ToolResultMessage(call.ID, call.Function.Name, "already executed")
	failedMarker := TurnFailureContextMessage("turn-b")
	encodedMarker, err := json.Marshal(failedMarker)
	if err != nil {
		t.Fatal(err)
	}
	var restoredMarker Message
	if err := json.Unmarshal(encodedMarker, &restoredMarker); err != nil {
		t.Fatal(err)
	}
	if !IsTurnIncompleteContextMessage(restoredMarker) || IsTurnCancellationContextMessage(restoredMarker) || restoredMarker.Provenance.Reference != "turn-b" {
		t.Fatalf("failed-turn marker did not survive durable message serialization: %#v", restoredMarker)
	}
	latestC := UserMessageWithSource("解释为什么核对文件要保留原始依据", UserNameHuman, MessageSource{Kind: MessageSourceUser, Form: MessageFormRequest}, &MessageProvenance{Producer: "human", Reference: "turn-c"})
	history := []Message{completedA, completedReply, failedB, toolAssistant, toolResult, failedMarker, latestC}

	wire := MessagesWithSystem("base", history)
	if len(wire) != 7 {
		t.Fatalf("failed turn projection length=%d, want system plus six canonical messages: %#v", len(wire), wire)
	}
	if !strings.Contains(wire[0].Content, "优先响应最新用户消息") || !strings.Contains(wire[0].Content, "明确要求恢复") {
		t.Fatalf("failed-turn guidance missing from provider system message: %q", wire[0].Content)
	}
	if !strings.HasPrefix(wire[3].Content, turnFailureUserPrefix) || !strings.HasSuffix(wire[3].Content, failedB.Content) {
		t.Fatalf("failed user request was not marked in provider projection: %#v", wire[3])
	}
	if len(wire[4].ToolCalls) != 1 || wire[4].ToolCalls[0].ID != call.ID || wire[5].Role != "tool" || wire[5].ToolCallID != call.ID {
		t.Fatalf("completed tool call/result pairing was changed: %#v", wire)
	}
	if wire[6].Content != latestC.Content {
		t.Fatalf("latest intent was changed: %#v", wire[6])
	}
	if err := ValidateToolProtocol(wire); err != nil {
		t.Fatalf("provider history with failed marker has invalid tool protocol: %v", err)
	}

	continued := append(append([]Message(nil), history...), UserMessage("明确继续之前的配额分类探针", UserNameHuman))
	continuedWire := MessagesWithSystem("base", continued)
	if !strings.Contains(continuedWire[0].Content, "明确要求恢复") || !strings.HasSuffix(continuedWire[len(continuedWire)-1].Content, "明确继续之前的配额分类探针") {
		t.Fatalf("explicit continuation was not represented to provider: %#v", continuedWire)
	}
	if strings.Contains(continuedWire[3].Content, "已取消") || !strings.Contains(continuedWire[3].Content, "未成功完成") {
		t.Fatalf("failure marker was misrepresented as cancellation: %#v", continuedWire[3])
	}
}

func TestPartialAssistantMessageRemainsPlainTextForNextModelTurn(t *testing.T) {
	partial := Message{
		Role:    "assistant",
		Content: "partial answer",
		Source:  &MessageSource{Kind: MessageSourceModel},
		Provenance: &MessageProvenance{
			Producer: "model", Operation: "partial_response", Reference: "turn-1/step-1",
		},
	}
	if !IsPartialAssistantMessage(partial) {
		t.Fatal("partial assistant marker was not recognized")
	}
	payloads, err := MessagesToAPIPayload([]Message{{Role: "user", Content: "continue"}, partial})
	if err != nil {
		t.Fatal(err)
	}
	if payloads[1]["role"] != "assistant" || payloads[1]["content"] != "partial answer" {
		t.Fatalf("provider partial payload = %#v", payloads[1])
	}
	if _, ok := payloads[1]["source"]; ok {
		t.Fatalf("internal source leaked to provider: %#v", payloads[1])
	}
}

func TestConversationBoundaryIsOnlyHiddenForIdleContextProducer(t *testing.T) {
	m := UserMessage("private bridge", UserNameConversationBoundary)
	if m.Source == nil || m.Source.Kind != MessageSourceRuntime || m.Source.Form != MessageFormNotice || m.Provenance == nil || m.Provenance.Producer != "idle_context" {
		t.Fatalf("boundary metadata = %#v / %#v", m.Source, m.Provenance)
	}
	if !IsHiddenInjectedUserMessage(m) {
		t.Fatal("idle context bridge should be hidden")
	}
	other := UserMessageWithSource("runtime content", "runtime", MessageSource{Kind: MessageSourceRuntime, Form: MessageFormNotice}, &MessageProvenance{Producer: "other_runtime"})
	if IsHiddenInjectedUserMessage(other) {
		t.Fatal("other runtime user content must remain visible")
	}
}
