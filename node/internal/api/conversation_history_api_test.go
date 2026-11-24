package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type countingConversationHistoryLLM struct {
	inner       *llm.MockClient
	streamCalls atomic.Int64
}

func (c *countingConversationHistoryLLM) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.streamCalls.Add(1)
	return c.inner.StreamChat(ctx, req, handler)
}

func (c *countingConversationHistoryLLM) CompleteText(ctx context.Context, req llm.CompleteRequest) (string, error) {
	return c.inner.CompleteText(ctx, req)
}

func (c *countingConversationHistoryLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return c.inner.NormalizeAssistant(existing, msg)
}

type conversationHistoryToolLLM struct{ calls atomic.Int64 }

func (c *conversationHistoryToolLLM) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.calls.Add(1)
	for _, message := range req.Messages {
		if message.Role == "tool" {
			if handler.OnDelta != nil {
				handler.OnDelta("Tool history complete")
			}
			return llm.ChatResult{Content: "Tool history complete", FinishReason: "stop"}, nil
		}
	}
	args, err := json.Marshal(map[string]string{
		"path":         "history-api-tool.txt",
		"content":      "tool result for API history",
		"call_purpose": "verify tool-step history API",
	})
	if err != nil {
		return llm.ChatResult{}, err
	}
	return llm.ChatResult{ToolCalls: []llm.ToolCall{{
		ID: "history-api-write-call", Type: "function",
		Function: llm.ToolCallFunction{Name: "write_file", Arguments: string(args)},
	}}, FinishReason: "tool_calls"}, nil
}

func (*conversationHistoryToolLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*conversationHistoryToolLLM) NormalizeAssistant(existing []llm.Message, message llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, message)
}

func TestConversationHistoryRoutesValidateScopeAndPageLimits(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "conversation-history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := NewServer(testConfig(t), nil, WithStore(st), WithSkipStore())
	defer srv.sessions.Stop()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cases := []struct {
		path       string
		wantStatus int
		wantCode   string
	}{
		{path: "/v1/agents/a/conversations/b/rounds", wantStatus: http.StatusNotFound, wantCode: "conversation_not_found"},
		{path: "/v1/agents/a/conversations/a/rounds?limit=101", wantStatus: http.StatusBadRequest, wantCode: "invalid_history_limit"},
		{path: "/v1/agents/a/conversations/a/rounds", wantStatus: http.StatusNotFound, wantCode: "conversation_history_not_found"},
		{path: "/v1/agents/a/conversations/a/rounds/r1/revisions", wantStatus: http.StatusNotFound, wantCode: "conversation_history_not_found"},
		{path: "/v1/agents/a/conversations/a/rounds/r1/items?limit=201", wantStatus: http.StatusBadRequest, wantCode: "invalid_history_limit"},
		{path: "/v1/agents/a/conversations/a/rounds/r1/tool-steps", wantStatus: http.StatusBadRequest, wantCode: "invalid_history_cursor"},
		{path: "/v1/agents/a/conversations/a/rounds/r1/tool-steps/call-1", wantStatus: http.StatusBadRequest, wantCode: "invalid_history_cursor"},
		{path: "/v1/agents/a/conversations/a/rounds/r1/items/item-1/content?part=other", wantStatus: http.StatusBadRequest, wantCode: "invalid_history_part"},
		{path: "/v1/agents/a/conversations/a/legacy-items?limit=201", wantStatus: http.StatusBadRequest, wantCode: "invalid_history_limit"},
	}
	for _, tc := range cases {
		resp, err := http.Get(ts.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			t.Fatalf("decode %s: %v", tc.path, decodeErr)
		}
		if resp.StatusCode != tc.wantStatus || body.Error.Code != tc.wantCode {
			t.Fatalf("GET %s status=%d code=%q, want status=%d code=%q", tc.path, resp.StatusCode, body.Error.Code, tc.wantStatus, tc.wantCode)
		}
	}
}

func TestConversationHistoryRouteReturnsUnassignedLegacyRecords(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "conversation-history-legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.PersistRuntimeDelta(context.Background(), store.Record{AgentID: "agent-legacy", NodeID: "node"}, &store.ConversationDelta{
		ConversationID: "agent-legacy",
		Upserts: []llm.Message{
			{Role: "user", Content: "legacy question", RecordID: "legacy-question"},
			{Role: "assistant", Content: "legacy answer", RecordID: "legacy-answer"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(testConfig(t), nil, WithStore(st), WithSkipStore())
	defer srv.sessions.Stop()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/agents/agent-legacy/conversations/agent-legacy/legacy-items?limit=1")
	if err != nil {
		t.Fatal(err)
	}
	var first conversationLegacyItemsResponse
	decodeErr := json.NewDecoder(resp.Body).Decode(&first)
	resp.Body.Close()
	if decodeErr != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy page status=%d page=%+v decodeErr=%v", resp.StatusCode, first, decodeErr)
	}
	if first.HistoryCompleteness != "complete" || len(first.Items) != 1 || !first.HasMore || first.NextAfterItemID != "legacy-question" {
		t.Fatalf("unexpected legacy page=%+v", first)
	}
	kind, _ := first.Items[0]["kind"].(string)
	text, _ := first.Items[0]["text"].(string)
	if kind != "user" || text != "legacy question" {
		t.Fatalf("unsafe or malformed legacy transcript entry=%+v", first.Items[0])
	}
	resp, err = http.Get(ts.URL + "/v1/agents/agent-legacy/conversations/agent-legacy/legacy-items?after_item_id=" + first.NextAfterItemID)
	if err != nil {
		t.Fatal(err)
	}
	var next conversationLegacyItemsResponse
	decodeErr = json.NewDecoder(resp.Body).Decode(&next)
	resp.Body.Close()
	if decodeErr != nil || resp.StatusCode != http.StatusOK || len(next.Items) != 1 || next.HasMore {
		t.Fatalf("next legacy page status=%d page=%+v decodeErr=%v", resp.StatusCode, next, decodeErr)
	}
}

func TestConversationHistoryRoutesReturnReadOnlyRoundAndItems(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "conversation-history-live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	model := &countingConversationHistoryLLM{inner: &llm.MockClient{}}
	srv := NewServer(testConfig(t), nil, WithLLM(model), WithStore(st), WithSkipStore())
	defer srv.sessions.Stop()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	agentID := createTestRuntime(t, srv)
	hydrate, err := srv.sessions.GetHydrateView(agentID)
	if err != nil {
		t.Fatal(err)
	}
	req := postMessageRequest{Content: "history api round"}
	digest, err := digestMessageRequest(req, "message")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.sessions.EnqueueMessageWithIdempotency(t.Context(), agentID, req.Content, nil, nil, "", "history-api-request", digest, hydrate.ConversationEpoch); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := st.LoadConversationRoundPage(context.Background(), agentID, "", 10)
		if err == nil && len(page.Rounds) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for persisted conversation round (page=%+v err=%v)", page, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitSessionIdleDeadline(t, srv, agentID, 5*time.Second)
	if got := model.streamCalls.Load(); got != 1 {
		t.Fatalf("initial turn model calls=%d, want 1", got)
	}

	resp, err := http.Get(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds", ts.URL, agentID, agentID))
	if err != nil {
		t.Fatal(err)
	}
	var rounds store.ConversationRoundPage
	decodeErr := json.NewDecoder(resp.Body).Decode(&rounds)
	resp.Body.Close()
	if decodeErr != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("round page status=%d page=%+v decodeErr=%v", resp.StatusCode, rounds, decodeErr)
	}
	if rounds.AgentID != agentID || rounds.ConversationID != agentID || len(rounds.Rounds) != 1 || rounds.Rounds[0].ItemCount < 2 {
		t.Fatalf("unexpected round history page=%+v", rounds)
	}
	roundID := rounds.Rounds[0].RoundID
	resp, err = http.Get(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/items", ts.URL, agentID, agentID, roundID))
	if err != nil {
		t.Fatal(err)
	}
	var items conversationRoundItemsResponse
	decodeErr = json.NewDecoder(resp.Body).Decode(&items)
	resp.Body.Close()
	if decodeErr != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("round items status=%d page=%+v decodeErr=%v", resp.StatusCode, items, decodeErr)
	}
	if !items.Selected || items.RevisionStatus == "active" || len(items.Items) < 2 {
		t.Fatalf("unexpected round items page=%+v", items)
	}
	firstKind, _ := items.Items[0]["kind"].(string)
	firstText, _ := items.Items[0]["text"].(string)
	if firstKind != "user" || firstText != req.Content {
		t.Fatalf("unexpected round items page=%+v", items)
	}
	resp, err = http.Get(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/revisions", ts.URL, agentID, agentID, roundID))
	if err != nil {
		t.Fatal(err)
	}
	var revisions conversationRoundRevisionsResponse
	decodeErr = json.NewDecoder(resp.Body).Decode(&revisions)
	resp.Body.Close()
	if decodeErr != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("round revisions status=%d page=%+v decodeErr=%v", resp.StatusCode, revisions, decodeErr)
	}
	if revisions.AgentID != agentID || revisions.RoundID != roundID || len(revisions.Revisions) != 1 || !revisions.Revisions[0].Selected || revisions.Revisions[0].RevisionID != revisions.SelectedRevisionID {
		t.Fatalf("unexpected revision list=%+v", revisions)
	}
	oldRevisionID := revisions.SelectedRevisionID
	var target conversationResubmitTargetResponse
	targetReady := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		resp, err = http.Get(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/resubmit-target?revision_id=%s", ts.URL, agentID, agentID, roundID, oldRevisionID))
		if err != nil {
			t.Fatal(err)
		}
		decodeErr = json.NewDecoder(resp.Body).Decode(&target)
		status := resp.StatusCode
		resp.Body.Close()
		if status == http.StatusOK && decodeErr == nil {
			targetReady = true
			break
		}
		if status != http.StatusConflict {
			t.Fatalf("resubmit target status=%d target=%+v decodeErr=%v", status, target, decodeErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !targetReady {
		t.Fatalf("resubmit target did not become available: target=%+v decodeErr=%v", target, decodeErr)
	}
	if target.Input.Content != req.Content || target.RoundID != roundID || target.RevisionID != oldRevisionID || target.ConversationEpoch == 0 || target.ConversationRevision == 0 {
		t.Fatalf("unexpected resubmit target=%+v", target)
	}
	resubmitBody, err := json.Marshal(conversationResubmitRequest{
		ExpectedSelectedRevisionID: oldRevisionID, ExpectedConversationRevision: target.ConversationRevision,
		PreCheckpointRevision: target.PreCheckpointRevision, ConversationEpoch: target.ConversationEpoch,
		ClientRequestID: "history-api-resubmit", Content: "edited history api round",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.Post(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/resubmit", ts.URL, agentID, agentID, roundID), "application/json", bytes.NewReader(resubmitBody))
	if err != nil {
		t.Fatal(err)
	}
	var accepted postMessageResponse
	decodeErr = json.NewDecoder(resp.Body).Decode(&accepted)
	resp.Body.Close()
	if decodeErr != nil || resp.StatusCode != http.StatusOK || !accepted.Accepted || accepted.InputSeq == 0 {
		t.Fatalf("resubmit status=%d accepted=%+v decodeErr=%v", resp.StatusCode, accepted, decodeErr)
	}
	var resubmittedRevisions store.ConversationRevisionPage
	resubmitFinished := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		resubmittedRevisions, err = st.LoadConversationRoundRevisions(context.Background(), agentID, roundID)
		if err == nil && len(resubmittedRevisions.Revisions) == 2 && resubmittedRevisions.SelectedRevisionID != oldRevisionID {
			resubmitFinished = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !resubmitFinished {
		t.Fatalf("resubmitted revision did not finish: page=%+v err=%v", resubmittedRevisions, err)
	}
	waitSessionIdleDeadline(t, srv, agentID, 5*time.Second)
	if got := model.streamCalls.Load(); got != 2 {
		t.Fatalf("initial + resubmitted turns model calls=%d, want 2", got)
	}
	resp, err = http.Post(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/resubmit", ts.URL, agentID, agentID, roundID), "application/json", bytes.NewReader(resubmitBody))
	if err != nil {
		t.Fatal(err)
	}
	var duplicate postMessageResponse
	decodeErr = json.NewDecoder(resp.Body).Decode(&duplicate)
	resp.Body.Close()
	if decodeErr != nil || resp.StatusCode != http.StatusOK || !duplicate.Accepted || !duplicate.Duplicate || duplicate.InputSeq != accepted.InputSeq {
		t.Fatalf("resubmit duplicate status=%d response=%+v decodeErr=%v", resp.StatusCode, duplicate, decodeErr)
	}
	if got := model.streamCalls.Load(); got != 2 {
		t.Fatalf("duplicate resubmit executed another model turn: calls=%d", got)
	}
	var changedRequest conversationResubmitRequest
	if err := json.Unmarshal(resubmitBody, &changedRequest); err != nil {
		t.Fatal(err)
	}
	changedRequest.Content = "different content with the same request ID"
	changedBody, err := json.Marshal(changedRequest)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.Post(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/resubmit", ts.URL, agentID, agentID, roundID), "application/json", bytes.NewReader(changedBody))
	if err != nil {
		t.Fatal(err)
	}
	var conflict struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeErr = json.NewDecoder(resp.Body).Decode(&conflict)
	resp.Body.Close()
	if decodeErr != nil || resp.StatusCode != http.StatusConflict || conflict.Error.Code != "input_request_conflict" {
		t.Fatalf("changed duplicate resubmit status=%d error=%+v decodeErr=%v", resp.StatusCode, conflict, decodeErr)
	}
	if got := model.streamCalls.Load(); got != 2 {
		t.Fatalf("conflicting duplicate resubmit executed another model turn: calls=%d", got)
	}
	resp, err = http.Get(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/resubmit-target?revision_id=%s", ts.URL, agentID, agentID, roundID, oldRevisionID))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("superseded revision became editable again: status=%d", resp.StatusCode)
	}
	newItems, err := st.LoadConversationRoundItems(context.Background(), agentID, roundID, resubmittedRevisions.SelectedRevisionID, "", 20)
	if err != nil || len(newItems.Items) < 2 || newItems.Items[0].Payload.Content != "edited history api round" {
		t.Fatalf("new selected revision items=%+v err=%v", newItems, err)
	}
	oldItems, err := st.LoadConversationRoundItems(context.Background(), agentID, roundID, oldRevisionID, "", 20)
	if err != nil || len(oldItems.Items) < 2 || oldItems.Items[0].Payload.Content != req.Content || oldItems.Selected {
		t.Fatalf("old immutable revision items=%+v err=%v", oldItems, err)
	}
}

func TestConversationHistoryToolStepRoutesReturnSummaryAndOnDemandDetails(t *testing.T) {
	cfg := testConfig(t)
	registry, err := tools.NewRegistry(filepath.Join(cfg.RuntimeRoot, "workspace"), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(cfg.RuntimeRoot, "conversation-history-tool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	model := &conversationHistoryToolLLM{}
	srv := NewServer(cfg, nil,
		WithLLM(model), WithTools(registry),
		WithPolicy(policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeNever}})),
		WithStore(st), WithSkipStore(),
	)
	defer srv.sessions.Stop()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	agentID := createTestRuntime(t, srv)

	hydrate, err := srv.sessions.GetHydrateView(agentID)
	if err != nil {
		t.Fatal(err)
	}
	body := testIdempotentMessageBody(t, agentID, "run a safe file tool for history API validation")
	var messageRequest postMessageRequest
	if err := json.Unmarshal(body, &messageRequest); err != nil {
		t.Fatal(err)
	}
	messageRequest.ConversationEpoch = hydrate.ConversationEpoch
	body, err = json.Marshal(messageRequest)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(ts.URL+"/v1/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var accepted postMessageResponse
	decodeErr := json.NewDecoder(response.Body).Decode(&accepted)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || !accepted.Accepted {
		t.Fatalf("POST message status=%d response=%+v decodeErr=%v", response.StatusCode, accepted, decodeErr)
	}
	waitForConversationRoundCount(t, st, agentID, 1)
	waitSessionIdleDeadline(t, srv, agentID, 5*time.Second)
	if got := model.calls.Load(); got != 2 {
		t.Fatalf("model calls=%d, want first tool call and one final response", got)
	}

	var rounds store.ConversationRoundPage
	response, err = http.Get(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds", ts.URL, agentID, agentID))
	if err != nil {
		t.Fatal(err)
	}
	decodeErr = json.NewDecoder(response.Body).Decode(&rounds)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || len(rounds.Rounds) != 1 {
		t.Fatalf("rounds status=%d rounds=%+v decodeErr=%v", response.StatusCode, rounds.Rounds, decodeErr)
	}
	round := rounds.Rounds[0]
	var items conversationRoundItemsResponse
	response, err = http.Get(fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/items", ts.URL, agentID, agentID, round.RoundID))
	if err != nil {
		t.Fatal(err)
	}
	decodeErr = json.NewDecoder(response.Body).Decode(&items)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("round items status=%d decodeErr=%v", response.StatusCode, decodeErr)
	}
	var sourceItemID string
	for _, entry := range items.Items {
		if entry["kind"] == "tool_call" {
			sourceItemID, _ = entry["detail_item_id"].(string)
			break
		}
	}
	if sourceItemID == "" {
		t.Fatalf("tool-call entry with detail_item_id not found in %+v", items.Items)
	}
	stepsURL := fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/tool-steps?source_item_id=%s", ts.URL, agentID, agentID, round.RoundID, url.QueryEscape(sourceItemID))
	var steps conversationToolStepsResponse
	response, err = http.Get(stepsURL)
	if err != nil {
		t.Fatal(err)
	}
	decodeErr = json.NewDecoder(response.Body).Decode(&steps)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || steps.TotalCount != 1 || len(steps.Steps) != 1 || steps.HasMore {
		t.Fatalf("tool steps status=%d page=%+v decodeErr=%v", response.StatusCode, steps, decodeErr)
	}
	callData, _ := steps.Steps[0].CallEntry["data"].(map[string]any)
	callArgs, _ := callData["arguments"].(map[string]any)
	if callArgs["call_purpose"] != "verify tool-step history API" || callArgs["path"] != nil {
		t.Fatalf("summary step should expose purpose but not full arguments: %#v", callData)
	}
	if steps.Steps[0].ResultEntry["details_available"] != true {
		t.Fatalf("summary result should advertise on-demand details: %#v", steps.Steps[0].ResultEntry)
	}
	stepURL := fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/tool-steps/%s?source_item_id=%s", ts.URL, agentID, agentID, round.RoundID, url.PathEscape("history-api-write-call"), url.QueryEscape(sourceItemID))
	response, err = http.Get(stepURL)
	if err != nil {
		t.Fatal(err)
	}
	var detail conversationToolStepsResponse
	decodeErr = json.NewDecoder(response.Body).Decode(&detail)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || len(detail.Steps) != 1 {
		t.Fatalf("tool detail status=%d page=%+v decodeErr=%v", response.StatusCode, detail, decodeErr)
	}
	resultData, _ := detail.Steps[0].ResultEntry["data"].(map[string]any)
	resultArgs, _ := resultData["arguments"].(map[string]any)
	if resultData["status"] != "succeeded" || resultArgs["content"] != "tool result for API history" {
		t.Fatalf("on-demand tool result content=%#v", resultData)
	}
}
