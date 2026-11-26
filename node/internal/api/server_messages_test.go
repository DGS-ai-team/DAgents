package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

var testMessageRequestCounter atomic.Uint64

func testIdempotentMessageBody(t *testing.T, agentID, content string) []byte {
	t.Helper()
	body, err := json.Marshal(postMessageRequest{
		AgentID:           agentID,
		RequestType:       "message",
		Content:           content,
		ClientRequestID:   fmt.Sprintf("test-message-%d", testMessageRequestCounter.Add(1)),
		ConversationEpoch: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

type gatedMessageClient struct {
	started chan struct{}
	release chan struct{}
}

func (c *gatedMessageClient) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	select {
	case c.started <- struct{}{}:
	default:
	}
	select {
	case <-c.release:
		return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	}
}

func (*gatedMessageClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*gatedMessageClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestPostMessageReturnsConflictWhenResultCommitNeedsRecovery(t *testing.T) {
	cfg := testConfig(t)
	db, err := store.Open(filepath.Join(cfg.RuntimeDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := &gatedMessageClient{started: make(chan struct{}, 1), release: make(chan struct{})}
	registry, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(client), WithTools(registry), WithStore(db), WithSkipStore())
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	sessionID := createTestRuntime(t, srv)
	post := func(content string) (*http.Response, error) {
		body := testIdempotentMessageBody(t, sessionID, content)
		return http.Post(ts.URL+"/v1/messages", "application/json", bytes.NewReader(body))
	}
	first, err := post("trigger save failure")
	if err != nil {
		t.Fatal(err)
	}
	firstBody, _ := io.ReadAll(first.Body)
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("initial enqueue status = %d body=%s", first.StatusCode, firstBody)
	}
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("LLM execution did not start")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	close(client.release)

	deadline := time.Now().Add(3 * time.Second)
	recoveryObserved := false
	for time.Now().Before(deadline) {
		resp, getErr := http.Get(ts.URL + "/v1/agents/" + sessionID + "/hydrate")
		if getErr != nil {
			t.Fatal(getErr)
		}
		var hydrate struct {
			TranscriptSynchronized bool `json:"transcript_synchronized"`
			TurnState              struct {
				Phase              string `json:"phase"`
				RecoveryRequired   bool   `json:"recovery_required"`
				ResultCommitStatus string `json:"result_commit_status"`
			} `json:"turn_state"`
		}
		if resp.StatusCode != http.StatusOK {
			_, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			time.Sleep(20 * time.Millisecond)
			continue
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&hydrate)
		resp.Body.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if hydrate.TurnState.ResultCommitStatus == "failed" {
			if hydrate.TurnState.Phase != "recovery_required" || !hydrate.TurnState.RecoveryRequired {
				t.Fatalf("failed save hydrate did not expose recovery projection: %+v", hydrate.TurnState)
			}
			if hydrate.TranscriptSynchronized {
				t.Fatal("failed save hydrate claimed the in-memory transcript was synchronized")
			}
			recoveryObserved = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !recoveryObserved {
		t.Fatal("runtime did not expose failed result commit status")
	}

	second, err := post("must be rejected")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	body, _ := io.ReadAll(second.Body)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("recovery enqueue status = %d body=%s, want 409", second.StatusCode, body)
	}
	var got errorBody
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "result_commit_recovery_required" || got.Error.Message != "本轮结果需要恢复，暂不能接收新消息" {
		t.Fatalf("recovery API error = %+v", got.Error)
	}
}

func TestPostMessageRequiresIdempotencyIdentity(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	agentID := createTestRuntime(t, srv)

	for _, body := range []string{
		`{"agent_id":"` + agentID + `","request_type":"message","content":"missing both"}`,
		`{"agent_id":"` + agentID + `","request_type":"message","content":"missing epoch","client_request_id":"request-without-epoch"}`,
	} {
		response, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := io.ReadAll(response.Body)
		response.Body.Close()
		var got errorBody
		if response.StatusCode != http.StatusBadRequest || json.Unmarshal(payload, &got) != nil || got.Error.Code != "invalid_input_receipt" {
			t.Fatalf("missing idempotency identity status=%d body=%s", response.StatusCode, payload)
		}
	}
}

func TestPostMessageQueuesDuringActiveTurnAndHydrateReturnsStableInput(t *testing.T) {
	cfg := testConfig(t)
	db, err := store.Open(filepath.Join(cfg.RuntimeDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := &gatedMessageClient{started: make(chan struct{}, 2), release: make(chan struct{})}
	registry, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(client), WithTools(registry), WithStore(db), WithSkipStore())
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	sessionID := createTestRuntime(t, srv)

	post := func(content string) postMessageResponse {
		t.Helper()
		body := testIdempotentMessageBody(t, sessionID, content)
		response, err := http.Post(ts.URL+"/v1/messages", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			responseBody, _ := io.ReadAll(response.Body)
			t.Fatalf("post %q status=%d body=%s", content, response.StatusCode, responseBody)
		}
		var result postMessageResponse
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	first := post("first turn")
	if first.InputSeq == 0 {
		t.Fatal("first human input response omitted durable input_seq")
	}
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first model call did not start")
	}
	second := post("queued while working")
	if second.InputSeq == 0 || second.InputSeq == first.InputSeq {
		t.Fatalf("queued human input sequence = %d, first = %d", second.InputSeq, first.InputSeq)
	}

	var queued bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(ts.URL + "/v1/agents/" + sessionID + "/hydrate")
		if err != nil {
			t.Fatal(err)
		}
		var snapshot struct {
			PendingInputs []struct {
				InputID  string `json:"input_id"`
				InputSeq uint64 `json:"input_seq"`
				Status   string `json:"status"`
				Content  string `json:"content"`
			} `json:"pending_inputs"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&snapshot)
		response.Body.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		for _, pending := range snapshot.PendingInputs {
			if pending.InputSeq == second.InputSeq {
				if pending.InputID != sessionID+"/input/"+fmt.Sprint(second.InputSeq) || pending.Status != "queued" || pending.Content != "queued while working" {
					t.Fatalf("queued input projection = %+v", pending)
				}
				queued = true
				break
			}
		}
		if queued {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !queued {
		t.Fatal("hydrate did not expose the accepted queued human input")
	}

	close(client.release)
	select {
	case <-client.started:
	case <-time.After(5 * time.Second):
		t.Fatal("queued message was not dispatched after the active Turn completed")
	}
	transcripted := false
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(ts.URL + "/v1/agents/" + sessionID + "/hydrate")
		if err != nil {
			t.Fatal(err)
		}
		var snapshot struct {
			Transcript []struct {
				ItemID string `json:"item_id"`
				Kind   string `json:"kind"`
				Text   string `json:"text"`
			} `json:"transcript"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&snapshot)
		response.Body.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		for _, entry := range snapshot.Transcript {
			if entry.ItemID == sessionID+"/input/"+fmt.Sprint(second.InputSeq) && entry.Kind == "user" && entry.Text == "queued while working" {
				transcripted = true
				break
			}
		}
		if transcripted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !transcripted {
		t.Fatal("queued input did not become a transcript item with the acknowledged identity")
	}
}

func TestStopTurnKeepsQueuedMessagePausedUntilExplicitResume(t *testing.T) {
	cfg := testConfig(t)
	db, err := store.Open(filepath.Join(cfg.RuntimeDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := &gatedMessageClient{started: make(chan struct{}, 2), release: make(chan struct{})}
	registry, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(client), WithTools(registry), WithStore(db), WithSkipStore())
	t.Cleanup(func() {
		srv.Close()
		_ = db.Close()
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	sessionID := createTestRuntime(t, srv)

	post := func(content string) postMessageResponse {
		t.Helper()
		resp, err := http.Post(ts.URL+"/v1/messages", "application/json", bytes.NewReader(testIdempotentMessageBody(t, sessionID, content)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("post %q status=%d body=%s", content, resp.StatusCode, body)
		}
		var accepted postMessageResponse
		if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
			t.Fatal(err)
		}
		return accepted
	}
	post("stop the active turn")
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("active LLM call did not start")
	}
	queued := post("remain queued until resumed")

	stop, err := http.Post(ts.URL+"/v1/agents/"+sessionID+"/cancel", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var stopped cancelTurnResponse
	if err := json.NewDecoder(stop.Body).Decode(&stopped); err != nil {
		stop.Body.Close()
		t.Fatal(err)
	}
	stop.Body.Close()
	if stop.StatusCode != http.StatusOK || !stopped.Cancelled || !stopped.Terminal || !stopped.QueuePaused || stopped.QueueControlRevision != 2 {
		t.Fatalf("stop response status=%d value=%+v", stop.StatusCode, stopped)
	}

	type hydrateResponse struct {
		QueuePaused          bool   `json:"queue_paused"`
		QueueControlRevision uint64 `json:"queue_control_revision"`
		PendingInputs        []struct {
			InputSeq uint64 `json:"input_seq"`
			Status   string `json:"status"`
			Content  string `json:"content"`
		} `json:"pending_inputs"`
	}
	hydrate := func() hydrateResponse {
		t.Helper()
		resp, err := http.Get(ts.URL + "/v1/agents/" + sessionID + "/hydrate")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var snapshot hydrateResponse
		if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	deadline := time.Now().Add(3 * time.Second)
	var snapshot hydrateResponse
	for time.Now().Before(deadline) {
		snapshot = hydrate()
		if len(snapshot.PendingInputs) == 1 && snapshot.PendingInputs[0].InputSeq == queued.InputSeq {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !snapshot.QueuePaused || snapshot.QueueControlRevision != 2 || len(snapshot.PendingInputs) != 1 || snapshot.PendingInputs[0].Status != "queued" || snapshot.PendingInputs[0].Content != "remain queued until resumed" {
		t.Fatalf("paused hydrate = %+v", snapshot)
	}
	select {
	case <-client.started:
		t.Fatal("queued message started before explicit resume")
	case <-time.After(150 * time.Millisecond):
	}

	resumeBody, _ := json.Marshal(map[string]any{"paused": false, "expected_revision": snapshot.QueueControlRevision})
	resume, err := http.Post(ts.URL+"/v1/agents/"+sessionID+"/inputs/control", "application/json", bytes.NewReader(resumeBody))
	if err != nil {
		t.Fatal(err)
	}
	var resumed struct {
		Paused   bool   `json:"queue_paused"`
		Revision uint64 `json:"queue_control_revision"`
	}
	if err := json.NewDecoder(resume.Body).Decode(&resumed); err != nil {
		resume.Body.Close()
		t.Fatal(err)
	}
	resume.Body.Close()
	if resume.StatusCode != http.StatusOK || resumed.Paused || resumed.Revision != 3 {
		t.Fatalf("resume status=%d value=%+v", resume.StatusCode, resumed)
	}
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("queued message did not start after explicit resume")
	}
	close(client.release)
	waitSessionIdle(t, srv, sessionID)
	final := hydrate()
	if final.QueuePaused || len(final.PendingInputs) != 0 {
		t.Fatalf("final hydrate retained paused or pending queue: %+v", final)
	}
}

func TestPostMessageIdempotencyReceiptSurvivesConsumptionAndClearFencesOldRequest(t *testing.T) {
	cfg := testConfig(t)
	db, err := store.Open(filepath.Join(cfg.RuntimeDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := &gatedMessageClient{started: make(chan struct{}, 2), release: make(chan struct{})}
	registry, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(client), WithTools(registry), WithStore(db), WithSkipStore())
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	agentID := createTestRuntime(t, srv)
	const requestID = "desktop-send-idempotency-1"
	post := func(content string, epoch uint64) (*http.Response, error) {
		t.Helper()
		body, marshalErr := json.Marshal(postMessageRequest{
			AgentID: agentID, RequestType: "message", Content: content,
			ClientRequestID: requestID, ConversationEpoch: epoch,
		})
		if marshalErr != nil {
			return nil, marshalErr
		}
		return http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(string(body)))
	}
	decodeAccepted := func(response *http.Response) postMessageResponse {
		t.Helper()
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("message status=%d body=%s", response.StatusCode, body)
		}
		var result postMessageResponse
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	firstResponse, err := post("exactly once at intake", 1)
	if err != nil {
		t.Fatal(err)
	}
	first := decodeAccepted(firstResponse)
	if first.InputSeq == 0 || first.Duplicate {
		t.Fatalf("first submission response = %+v", first)
	}
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first model call did not start")
	}

	duplicateResponse, err := post("exactly once at intake", 1)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := decodeAccepted(duplicateResponse)
	if !duplicate.Duplicate || duplicate.InputSeq != first.InputSeq {
		t.Fatalf("replayed submission response = %+v, first = %+v", duplicate, first)
	}

	conflictResponse, err := post("different payload", 1)
	if err != nil {
		t.Fatal(err)
	}
	conflictBody, _ := io.ReadAll(conflictResponse.Body)
	conflictResponse.Body.Close()
	var conflict errorBody
	if conflictResponse.StatusCode != http.StatusConflict || json.Unmarshal(conflictBody, &conflict) != nil || conflict.Error.Code != "input_request_conflict" {
		t.Fatalf("changed payload status=%d body=%s", conflictResponse.StatusCode, conflictBody)
	}

	close(client.release)
	inputsURL := ts.URL + "/v1/agents/" + agentID + "/inputs?client_request_id=" + requestID + "&conversation_epoch=1"
	deadline := time.Now().Add(5 * time.Second)
	consumed := false
	for time.Now().Before(deadline) {
		response, getErr := http.Get(inputsURL)
		if getErr != nil {
			t.Fatal(getErr)
		}
		var receipt inputReceiptResponse
		decodeErr := json.NewDecoder(response.Body).Decode(&receipt)
		response.Body.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if receipt.Found && receipt.Status == "consumed" && receipt.InputSeq == first.InputSeq {
			consumed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !consumed {
		t.Fatal("receipt was not retained as consumed after the input was acknowledged")
	}

	clearResponse, err := http.Post(ts.URL+"/v1/agents/"+agentID+"/clear-context", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	clearResponse.Body.Close()
	if clearResponse.StatusCode != http.StatusOK {
		t.Fatalf("clear context status=%d", clearResponse.StatusCode)
	}
	oldEpochResponse, err := post("exactly once at intake", 1)
	if err != nil {
		t.Fatal(err)
	}
	oldEpochBody, _ := io.ReadAll(oldEpochResponse.Body)
	oldEpochResponse.Body.Close()
	var oldEpochError errorBody
	if oldEpochResponse.StatusCode != http.StatusConflict || json.Unmarshal(oldEpochBody, &oldEpochError) != nil || oldEpochError.Error.Code != "conversation_changed" {
		t.Fatalf("old conversation submission status=%d body=%s", oldEpochResponse.StatusCode, oldEpochBody)
	}
}

func TestQueuedInputEditAndDeleteUseRevisionCAS(t *testing.T) {
	cfg := testConfig(t)
	db, err := store.Open(filepath.Join(cfg.RuntimeDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := &gatedMessageClient{started: make(chan struct{}, 2), release: make(chan struct{})}
	registry, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(client), WithTools(registry), WithStore(db), WithSkipStore())
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	sessionID := createTestRuntime(t, srv)
	post := func(content string) postMessageResponse {
		t.Helper()
		request := postMessageRequest{
			AgentID: sessionID, RequestType: "message", Content: content,
			FileReferences:  []llm.FileReference{{Path: "reports/plan.csv", Name: "plan.csv"}},
			ClientRequestID: fmt.Sprintf("test-message-%d", testMessageRequestCounter.Add(1)), ConversationEpoch: 1,
		}
		body, marshalErr := json.Marshal(request)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		response, err := http.Post(ts.URL+"/v1/messages", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			responseBody, _ := io.ReadAll(response.Body)
			t.Fatalf("post %q status=%d body=%s", content, response.StatusCode, responseBody)
		}
		var result postMessageResponse
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	post("active turn")
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first model call did not start")
	}
	queued := post("before edit")
	path := ts.URL + "/v1/agents/" + sessionID + "/inputs/" + fmt.Sprint(queued.InputSeq)
	patch := func(revision uint64, content string) *http.Response {
		t.Helper()
		body := fmt.Sprintf(`{"expected_revision":%d,"content":%q}`, revision, content)
		req, err := http.NewRequest(http.MethodPatch, path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	updated := patch(1, "edited while working")
	if updated.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(updated.Body)
		updated.Body.Close()
		t.Fatalf("edit status=%d body=%s", updated.StatusCode, body)
	}
	var edited queuedInputResponse
	if err := json.NewDecoder(updated.Body).Decode(&edited); err != nil {
		t.Fatal(err)
	}
	updated.Body.Close()
	if edited.InputSeq != queued.InputSeq || edited.InputID != sessionID+"/input/"+fmt.Sprint(queued.InputSeq) || edited.Revision != 2 || edited.Status != "queued" {
		t.Fatalf("edited receipt=%+v", edited)
	}
	hydrateResponse, err := http.Get(ts.URL + "/v1/agents/" + sessionID + "/hydrate")
	if err != nil {
		t.Fatal(err)
	}
	var hydrated struct {
		PendingInputs []struct {
			InputSeq       uint64              `json:"input_seq"`
			InputRevision  uint64              `json:"input_revision"`
			FileReferences []llm.FileReference `json:"file_refs"`
		} `json:"pending_inputs"`
	}
	if err := json.NewDecoder(hydrateResponse.Body).Decode(&hydrated); err != nil {
		t.Fatal(err)
	}
	hydrateResponse.Body.Close()
	attachmentPreserved := false
	for _, pending := range hydrated.PendingInputs {
		if pending.InputSeq == queued.InputSeq && pending.InputRevision == 2 && len(pending.FileReferences) == 1 && pending.FileReferences[0].Path == "reports/plan.csv" {
			attachmentPreserved = true
		}
	}
	if !attachmentPreserved {
		t.Fatalf("text-only edit did not preserve the queued attachment: %+v", hydrated.PendingInputs)
	}
	stale := patch(1, "stale edit")
	staleBody, _ := io.ReadAll(stale.Body)
	stale.Body.Close()
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("stale edit status=%d body=%s, want 409", stale.StatusCode, staleBody)
	}
	var staleError errorBody
	if err := json.Unmarshal(staleBody, &staleError); err != nil {
		t.Fatal(err)
	}
	if staleError.Error.Code != "input_revision_conflict" {
		t.Fatalf("stale edit error=%+v", staleError.Error)
	}
	deleteBody := strings.NewReader(`{"expected_revision":2}`)
	deleteReq, err := http.NewRequest(http.MethodDelete, path, deleteBody)
	if err != nil {
		t.Fatal(err)
	}
	deleteReq.Header.Set("Content-Type", "application/json")
	deleted, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	deletedBody, _ := io.ReadAll(deleted.Body)
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s, want 204", deleted.StatusCode, deletedBody)
	}
	close(client.release)
	select {
	case <-client.started:
		t.Fatal("withdrawn input unexpectedly started an LLM call")
	case <-time.After(150 * time.Millisecond):
	}
}
