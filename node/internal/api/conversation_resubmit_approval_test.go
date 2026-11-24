package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type resubmitApprovalLLM struct {
	toolCalls atomic.Int32
}

func (c *resubmitApprovalLLM) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	latestUser := ""
	toolResultAfterLatestUser := false
	for _, message := range req.Messages {
		if message.Role == "user" {
			latestUser = message.Content
			toolResultAfterLatestUser = false
			continue
		}
		if latestUser != "" && message.Role == "tool" {
			toolResultAfterLatestUser = true
		}
	}
	if latestUser == "" {
		return llm.ChatResult{}, fmt.Errorf("test model received no user message")
	}
	if toolResultAfterLatestUser {
		content := "Finished: " + latestUser
		if handler.OnDelta != nil {
			handler.OnDelta(content)
		}
		return llm.ChatResult{Content: content, FinishReason: "stop"}, nil
	}

	callNumber := c.toolCalls.Add(1)
	calls := make([]llm.ToolCall, 0, 2)
	for _, target := range []struct {
		suffix string
		path   string
	}{
		{suffix: "approved", path: "approval-effects.txt"},
		{suffix: "rejected", path: "rejection-effects.txt"},
	} {
		arguments, err := json.Marshal(map[string]string{
			"path":         target.path,
			"content":      latestUser,
			"call_purpose": "write the requested test effect",
		})
		if err != nil {
			return llm.ChatResult{}, err
		}
		calls = append(calls, llm.ToolCall{
			ID:   fmt.Sprintf("resubmit-write-%d-%s", callNumber, target.suffix),
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "write_file",
				Arguments: string(arguments),
			},
		})
	}
	externalArgs, err := json.Marshal(map[string]string{
		"target":       "release-note",
		"call_purpose": "publish the requested release note",
	})
	if err != nil {
		return llm.ChatResult{}, err
	}
	calls = append(calls, llm.ToolCall{
		ID:   fmt.Sprintf("resubmit-mcp-%d", callNumber),
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "mcp__qa__publish",
			Arguments: string(externalArgs),
		},
	})
	return llm.ChatResult{
		ToolCalls:    calls,
		FinishReason: "tool_calls",
	}, nil
}

func (*resubmitApprovalLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*resubmitApprovalLLM) NormalizeAssistant(existing []llm.Message, message llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, message)
}

func TestConversationResubmitRequiresFreshApprovalAndDoesNotRepeatRejectedWrite(t *testing.T) {
	cfg := testConfig(t)
	workspace := filepath.Join(cfg.RuntimeRoot, "workspace")
	registry, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	var externalSideEffects atomic.Int32
	if err := registry.SetMCPTools([]tools.MCPTool{{
		Name:        "mcp__qa__publish",
		Description: "publish a QA release note",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"target": map[string]any{"type": "string"},
		}},
		Call: func(_ context.Context, args json.RawMessage) (mcp.CallResult, error) {
			var payload struct {
				Target string `json:"target"`
			}
			if err := json.Unmarshal(args, &payload); err != nil {
				return mcp.CallResult{}, err
			}
			if payload.Target != "release-note" {
				return mcp.CallResult{}, fmt.Errorf("unexpected MCP target %q", payload.Target)
			}
			externalSideEffects.Add(1)
			return mcp.CallResult{Content: []mcp.ContentBlock{{Type: "text", Text: "published"}}}, nil
		},
	}}); err != nil {
		t.Fatal(err)
	}
	model := &resubmitApprovalLLM{}
	approvalPolicy := policy.NewEngineFromMaps(policy.Maps{
		Tools: map[string]policy.ApprovalMode{
			"write_file":       policy.ModeAlways,
			"mcp__qa__publish": policy.ModeAlways,
		},
	})
	conversationStore, err := store.Open(filepath.Join(cfg.RuntimeRoot, "conversation-resubmit-approval.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conversationStore.Close()
	srv := NewServer(cfg, nil,
		WithLLM(model), WithTools(registry), WithPolicy(approvalPolicy),
		WithStore(conversationStore), WithSkipStore(),
	)
	defer srv.sessions.Stop()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	agentID := createTestRuntime(t, srv)
	approvedFile := filepath.Join(workspace, "approval-effects.txt")
	rejectedFile := filepath.Join(workspace, "rejection-effects.txt")

	initialContent := "initial approved effect"
	initialHydrate, err := srv.sessions.GetHydrateView(agentID)
	if err != nil {
		t.Fatal(err)
	}
	initialRequest := postMessageRequest{Content: initialContent}
	initialDigest, err := digestMessageRequest(initialRequest, "message")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.sessions.EnqueueMessageWithIdempotency(
		t.Context(), agentID, initialRequest.Content, nil, nil, "", "resubmit-approval-initial",
		initialDigest, initialHydrate.ConversationEpoch,
	); err != nil {
		t.Fatal(err)
	}
	initialCallIDs := waitForPendingWriteApprovals(t, ts.URL, agentID)
	if len(initialCallIDs) != 2 {
		t.Fatalf("initial pending approvals=%v, want two", initialCallIDs)
	}
	initialMCPID := waitForPendingApprovalID(t, ts.URL, agentID, "mcp__qa__publish")
	if _, err := os.Stat(approvedFile); !os.IsNotExist(err) {
		t.Fatalf("write_file executed before approval: stat error=%v", err)
	}
	if _, err := os.Stat(rejectedFile); !os.IsNotExist(err) {
		t.Fatalf("second write_file executed before approval: stat error=%v", err)
	}
	resumeConversationApproval(t, ts.URL, agentID, []string{initialCallIDs[0], initialMCPID}, []string{initialCallIDs[1]})
	page := waitForConversationRoundCount(t, conversationStore, agentID, 1)
	waitSessionIdleDeadline(t, srv, agentID, 5*time.Second)
	if got, err := os.ReadFile(approvedFile); err != nil || string(got) != initialContent {
		t.Fatalf("approved initial side effect=%q err=%v", got, err)
	}
	if _, err := os.Stat(rejectedFile); !os.IsNotExist(err) {
		t.Fatalf("partially rejected initial side effect landed: stat error=%v", err)
	}
	if got := externalSideEffects.Load(); got != 1 {
		t.Fatalf("approved MCP side effect count=%d, want 1", got)
	}
	roundID := page.Rounds[0].RoundID
	revisions, err := conversationStore.LoadConversationRoundRevisions(context.Background(), agentID, roundID)
	if err != nil || len(revisions.Revisions) != 1 {
		t.Fatalf("initial revisions=%+v err=%v", revisions, err)
	}
	oldRevisionID := revisions.SelectedRevisionID
	target := waitForConversationResubmitTarget(t, ts.URL, agentID, roundID, oldRevisionID)

	editedContent := "edited version that must be rejected"
	resubmitBody, err := json.Marshal(conversationResubmitRequest{
		ExpectedSelectedRevisionID:   oldRevisionID,
		ExpectedConversationRevision: target.ConversationRevision,
		PreCheckpointRevision:        target.PreCheckpointRevision,
		ConversationEpoch:            target.ConversationEpoch,
		ClientRequestID:              "resubmit-approval-edited",
		Content:                      editedContent,
	})
	if err != nil {
		t.Fatal(err)
	}
	resubmitURL := fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/resubmit", ts.URL, agentID, agentID, roundID)
	response, err := http.Post(resubmitURL, "application/json", bytes.NewReader(resubmitBody))
	if err != nil {
		t.Fatal(err)
	}
	var accepted postMessageResponse
	decodeErr := json.NewDecoder(response.Body).Decode(&accepted)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || !accepted.Accepted {
		t.Fatalf("resubmit status=%d receipt=%+v decodeErr=%v", response.StatusCode, accepted, decodeErr)
	}

	resubmittedCallIDs := waitForPendingWriteApprovals(t, ts.URL, agentID)
	if len(resubmittedCallIDs) != 2 {
		t.Fatalf("resubmitted pending approvals=%v, want two", resubmittedCallIDs)
	}
	resubmittedMCPID := waitForPendingApprovalID(t, ts.URL, agentID, "mcp__qa__publish")
	for _, id := range resubmittedCallIDs {
		for _, oldID := range initialCallIDs {
			if id == oldID {
				t.Fatalf("resubmitted approval reused previous tool call ID %q", id)
			}
		}
	}
	for _, oldID := range append(append([]string(nil), initialCallIDs...), initialMCPID) {
		if resubmittedMCPID == oldID {
			t.Fatalf("resubmitted MCP approval reused previous tool call ID %q", resubmittedMCPID)
		}
	}
	if externalSideEffects.Load() != 1 {
		t.Fatalf("MCP side effect executed before fresh approval: count=%d", externalSideEffects.Load())
	}
	if got, err := os.ReadFile(approvedFile); err != nil || string(got) != initialContent {
		t.Fatalf("resubmitted write ran before fresh approval: content=%q err=%v", got, err)
	}
	if _, err := os.Stat(rejectedFile); !os.IsNotExist(err) {
		t.Fatalf("resubmitted write ran before fresh approval: stat error=%v", err)
	}
	resumeConversationApproval(t, ts.URL, agentID, nil, append(append([]string(nil), resubmittedCallIDs...), resubmittedMCPID))
	waitForConversationRevisionCount(t, conversationStore, agentID, roundID, 2)
	waitSessionIdleDeadline(t, srv, agentID, 5*time.Second)
	if got, err := os.ReadFile(approvedFile); err != nil || string(got) != initialContent {
		t.Fatalf("rejected resubmitted write changed the approved file: content=%q err=%v", got, err)
	}
	if _, err := os.Stat(rejectedFile); !os.IsNotExist(err) {
		t.Fatalf("rejected resubmitted write created file: stat error=%v", err)
	}
	if externalSideEffects.Load() != 1 {
		t.Fatalf("rejected MCP resubmission repeated external effect: count=%d", externalSideEffects.Load())
	}

	revisions, err = conversationStore.LoadConversationRoundRevisions(context.Background(), agentID, roundID)
	if err != nil || len(revisions.Revisions) != 2 {
		t.Fatalf("revisions before cancellation=%+v err=%v", revisions, err)
	}
	cancelTarget := waitForConversationResubmitTarget(t, ts.URL, agentID, roundID, revisions.SelectedRevisionID)
	postConversationResubmit(t, ts.URL, agentID, roundID, cancelTarget, "resubmit-approval-cancelled", "cancelled edit")
	cancelCallIDs := waitForPendingWriteApprovals(t, ts.URL, agentID)
	if len(cancelCallIDs) != 2 {
		t.Fatalf("cancel pending approvals=%v, want two", cancelCallIDs)
	}
	cancelMCPID := waitForPendingApprovalID(t, ts.URL, agentID, "mcp__qa__publish")
	for _, id := range append(append([]string(nil), cancelCallIDs...), cancelMCPID) {
		for _, oldIDs := range [][]string{initialCallIDs, resubmittedCallIDs} {
			for _, oldID := range oldIDs {
				if id == oldID {
					t.Fatalf("cancelled resubmission reused previous tool call ID %q", id)
				}
			}
		}
	}
	cancelURL := fmt.Sprintf("%s/v1/agents/%s/cancel", ts.URL, agentID)
	cancelResponse, err := http.Post(cancelURL, "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	cancelResponse.Body.Close()
	if cancelResponse.StatusCode != http.StatusOK {
		t.Fatalf("cancel pending approval status=%d", cancelResponse.StatusCode)
	}
	waitForConversationRevisionCount(t, conversationStore, agentID, roundID, 3)
	waitSessionIdleDeadline(t, srv, agentID, 5*time.Second)
	if got, err := os.ReadFile(approvedFile); err != nil || string(got) != initialContent {
		t.Fatalf("cancelled resubmitted write changed the approved file: content=%q err=%v", got, err)
	}
	if _, err := os.Stat(rejectedFile); !os.IsNotExist(err) {
		t.Fatalf("cancelled resubmitted write created file: stat error=%v", err)
	}
	if externalSideEffects.Load() != 1 {
		t.Fatalf("cancelled MCP resubmission repeated external effect: count=%d", externalSideEffects.Load())
	}
	if got := model.toolCalls.Load(); got != 3 {
		t.Fatalf("model proposed %d write batches, want one fresh batch per revision", got)
	}
}

func waitForConversationRoundCount(t *testing.T, conversationStore *store.SQLiteStore, agentID string, count int) store.ConversationRoundPage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, err := conversationStore.LoadConversationRoundPage(context.Background(), agentID, "", 10)
		if err == nil && len(page.Rounds) >= count {
			return page
		}
		time.Sleep(20 * time.Millisecond)
	}
	page, err := conversationStore.LoadConversationRoundPage(context.Background(), agentID, "", 10)
	t.Fatalf("timed out waiting for %d persisted rounds (page=%+v err=%v)", count, page, err)
	return page
}

func waitForConversationRevisionCount(t *testing.T, conversationStore *store.SQLiteStore, agentID, roundID string, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, err := conversationStore.LoadConversationRoundRevisions(context.Background(), agentID, roundID)
		if err == nil && len(page.Revisions) >= count && page.Revisions[count-1].Status != "active" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	page, err := conversationStore.LoadConversationRoundRevisions(context.Background(), agentID, roundID)
	t.Fatalf("timed out waiting for %d persisted revisions (page=%+v err=%v)", count, page, err)
}

func waitForPendingWriteApprovals(t *testing.T, baseURL, agentID string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(fmt.Sprintf("%s/v1/agents/%s/hydrate", baseURL, agentID))
		if err != nil {
			t.Fatal(err)
		}
		var hydrate struct {
			PendingHITL struct {
				Items []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"items"`
			} `json:"pending_hitl"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&hydrate)
		response.Body.Close()
		if decodeErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("hydrate status=%d err=%v", response.StatusCode, decodeErr)
		}
		ids := make([]string, 0, len(hydrate.PendingHITL.Items))
		for _, item := range hydrate.PendingHITL.Items {
			if item.Name == "write_file" && item.ID != "" {
				ids = append(ids, item.ID)
			}
		}
		if len(ids) == 2 {
			return ids
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for two write_file approvals")
	return nil
}

func waitForPendingApprovalID(t *testing.T, baseURL, agentID, toolName string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(fmt.Sprintf("%s/v1/agents/%s/hydrate", baseURL, agentID))
		if err != nil {
			t.Fatal(err)
		}
		var hydrate struct {
			PendingHITL struct {
				Items []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"items"`
			} `json:"pending_hitl"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&hydrate)
		status := response.StatusCode
		response.Body.Close()
		if decodeErr != nil || status != http.StatusOK {
			t.Fatalf("hydrate status=%d err=%v", status, decodeErr)
		}
		for _, item := range hydrate.PendingHITL.Items {
			if item.Name == toolName && item.ID != "" {
				return item.ID
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s approval", toolName)
	return ""
}

func resumeConversationApproval(t *testing.T, baseURL, agentID string, approved, rejected []string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"agent_id":     agentID,
		"request_type": "resume",
		"resume_value": map[string]any{
			"type":     "selection",
			"approved": approved,
			"rejected": rejected,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resume approval status=%d", response.StatusCode)
	}
}

func waitForConversationResubmitTarget(t *testing.T, baseURL, agentID, roundID, revisionID string) conversationResubmitTargetResponse {
	t.Helper()
	url := fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/resubmit-target?revision_id=%s", baseURL, agentID, agentID, roundID, revisionID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		var target conversationResubmitTargetResponse
		decodeErr := json.NewDecoder(response.Body).Decode(&target)
		status := response.StatusCode
		response.Body.Close()
		if status == http.StatusOK && decodeErr == nil {
			return target
		}
		if status != http.StatusConflict {
			t.Fatalf("resubmit target status=%d target=%+v err=%v", status, target, decodeErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for resubmit target")
	return conversationResubmitTargetResponse{}
}

func postConversationResubmit(t *testing.T, baseURL, agentID, roundID string, target conversationResubmitTargetResponse, requestID, content string) {
	t.Helper()
	body, err := json.Marshal(conversationResubmitRequest{
		ExpectedSelectedRevisionID:   target.RevisionID,
		ExpectedConversationRevision: target.ConversationRevision,
		PreCheckpointRevision:        target.PreCheckpointRevision,
		ConversationEpoch:            target.ConversationEpoch,
		ClientRequestID:              requestID,
		Content:                      content,
	})
	if err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/v1/agents/%s/conversations/%s/rounds/%s/resubmit", baseURL, agentID, agentID, roundID)
	response, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var receipt postMessageResponse
	decodeErr := json.NewDecoder(response.Body).Decode(&receipt)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || !receipt.Accepted {
		t.Fatalf("resubmit status=%d receipt=%+v err=%v", response.StatusCode, receipt, decodeErr)
	}
}
