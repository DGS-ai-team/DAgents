package workgroup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestWSURL(t *testing.T) {
	u, err := WSURL("http://127.0.0.1:8020")
	if err != nil {
		t.Fatal(err)
	}
	if u != "ws://127.0.0.1:8020/v1/workgroups/ws" {
		t.Fatalf("got %s", u)
	}
	u2, err := WSURL("https://manage.example/x/")
	if err != nil {
		t.Fatal(err)
	}
	if u2 != "wss://manage.example/x/v1/workgroups/ws" {
		t.Fatalf("got %s", u2)
	}
}

func TestDialerMultiResumeOffers(t *testing.T) {
	var (
		mu     sync.Mutex
		offers []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workgroups/ws" {
			http.NotFound(w, r)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "done")
		ctx := r.Context()
		var hello map[string]any
		if err := wsjson.Read(ctx, c, &hello); err != nil {
			return
		}
		_ = wsjson.Write(ctx, c, map[string]any{
			"type": "session.welcome",
			"payload": map[string]any{
				"node_id":               "node_b",
				"connection_generation": 1,
				"schema_version":        "0.5.0",
			},
		})
		for i := 0; i < 3; i++ {
			var frame map[string]any
			if err := wsjson.Read(ctx, c, &frame); err != nil {
				return
			}
			if frame["type"] != "resume.offer" {
				continue
			}
			mu.Lock()
			offers = append(offers, frame)
			mu.Unlock()
		}
		time.Sleep(80 * time.Millisecond)
	}))
	defer srv.Close()

	w := NewWorker(Config{NodeID: "node_b"})
	_ = w.Session.AckDelivery("wg_01h0000000000000000000000a", 5)
	_ = w.Session.AckDelivery("wg_01h0000000000000000000000b", 2)
	d := &Dialer{
		ManageURL:    srv.URL,
		NodeID:       "node_b",
		Worker:       w,
		WorkgroupIDs: []string{"wg_01h0000000000000000000000a"},
		ListWorkgroups: func(ctx context.Context) ([]string, error) {
			return []string{"wg_01h0000000000000000000000b", "wg_01h0000000000000000000000c"}, nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.ConnectAndServe(ctx) }()

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(offers)
		mu.Unlock()
		if n >= 3 {
			cancel()
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timeout offers=%d", n)
		case <-time.After(15 * time.Millisecond):
		}
	}
	<-errCh

	mu.Lock()
	defer mu.Unlock()
	if len(offers) != 3 {
		t.Fatalf("offers=%d", len(offers))
	}
	byWG := map[string]int64{}
	for _, o := range offers {
		p, _ := o["payload"].(map[string]any)
		wg, _ := p["workgroup_id"].(string)
		seq, _ := p["last_ack_delivery_seq"].(float64)
		byWG[wg] = int64(seq)
	}
	if byWG["wg_01h0000000000000000000000a"] != 5 {
		t.Fatalf("wg_a seq=%v", byWG)
	}
	if byWG["wg_01h0000000000000000000000b"] != 2 {
		t.Fatalf("wg_b seq=%v", byWG)
	}
	if byWG["wg_01h0000000000000000000000c"] != 0 {
		t.Fatalf("wg_c seq=%v", byWG)
	}
}

func TestDialerConnectProvisionCommand(t *testing.T) {
	dir := t.TempDir()
	var (
		mu       sync.Mutex
		gotHello bool
		results  []map[string]any
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workgroups/ws" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get(agentIDHeader) != "node_b" {
			http.Error(w, "missing agent", 400)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "done")
		ctx := r.Context()

		var hello map[string]any
		if err := wsjson.Read(ctx, c, &hello); err != nil {
			return
		}
		mu.Lock()
		gotHello = hello["type"] == "session.hello"
		mu.Unlock()

		_ = wsjson.Write(ctx, c, map[string]any{
			"type": "session.welcome",
			"payload": map[string]any{
				"node_id":               "node_b",
				"connection_generation": 1,
				"schema_version":        "0.5.0",
			},
		})

		digest := "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c"
		_ = wsjson.Write(ctx, c, WSEnvelope{
			EnvelopeID:           "en_01h00000000000000000000001",
			SchemaVersion:        SchemaVersion,
			Type:                 "member.provision",
			DeliverySeq:          1,
			ConnectionGeneration: 1,
			WorkgroupID:          "wg_01h00000000000000000000001",
			Payload: map[string]any{
				"provision_id":       "pv_01h00000000000000000000009",
				"workgroup_id":       "wg_01h00000000000000000000001",
				"member_id":          "mb_01h00000000000000000000002",
				"home_node_id":       "node_b",
				"member_spec_digest": digest,
				"lease_epoch":        2,
				"member_generation":  1,
				"tool_allow_names":   []string{"read_file"},
				"workspace_root":     filepath.Join(dir, "ws"),
			},
			SentAt: "2026-07-31T00:00:00Z",
		})

		// 等 provision_result
		var provRes map[string]any
		if err := wsjson.Read(ctx, c, &provRes); err != nil {
			return
		}
		mu.Lock()
		results = append(results, provRes)
		mu.Unlock()

		// 写 README 供 read_file（workspace 已由 Node 创建）
		payload, _ := provRes["payload"].(map[string]any)
		wsPath, _ := payload["workspace_path"].(string)
		_ = os.WriteFile(filepath.Join(wsPath, "README"), []byte("# Demo\n"), 0o644)
		rev, _ := payload["tool_catalog_revision"].(string)

		_ = wsjson.Write(ctx, c, WSEnvelope{
			EnvelopeID:           "en_01h00000000000000000000002",
			SchemaVersion:        SchemaVersion,
			Type:                 "tool.command",
			DeliverySeq:          2,
			ConnectionGeneration: 1,
			WorkgroupID:          "wg_01h00000000000000000000001",
			Payload: map[string]any{
				"command_id":            "cmd_01h00000000000000000000008",
				"workgroup_id":          "wg_01h00000000000000000000001",
				"member_id":             "mb_01h00000000000000000000002",
				"assign_id":             "as_01h00000000000000000000007",
				"run_id":                "rn_01h00000000000000000000006",
				"turn_id":               "tn_01h00000000000000000000005",
				"tool_call_id":          "call_1",
				"tool_name":             "read_file",
				"arguments_json":        `{"path":"README"}`,
				"payload_hash":          "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"lease_id":              "ls_01h00000000000000000000004",
				"lease_epoch":           2,
				"member_generation":     1,
				"member_spec_digest":    digest,
				"tool_catalog_revision": rev,
				"status":                "queued",
				"side_effect_class":     "fs_read",
			},
			SentAt: "2026-07-31T00:00:01Z",
		})

		var toolRes map[string]any
		if err := wsjson.Read(ctx, c, &toolRes); err != nil {
			return
		}
		mu.Lock()
		results = append(results, toolRes)
		mu.Unlock()

		// 保持连接片刻供 dialer 退出
		time.Sleep(50 * time.Millisecond)
	}))
	defer srv.Close()

	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	d := &Dialer{
		ManageURL: srv.URL,
		NodeID:    "node_b",
		Worker:    w,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.ConnectAndServe(ctx) }()

	deadline := time.After(4 * time.Second)
	for {
		mu.Lock()
		n := len(results)
		helloOK := gotHello
		mu.Unlock()
		if helloOK && n >= 2 {
			cancel()
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for dialer results")
		case <-time.After(20 * time.Millisecond):
		}
	}
	<-errCh

	mu.Lock()
	defer mu.Unlock()
	if !gotHello {
		t.Fatal("hello not received")
	}
	if len(results) < 2 {
		t.Fatalf("results=%d", len(results))
	}
	if results[0]["type"] != "member.provision_result" {
		raw, _ := json.Marshal(results[0])
		t.Fatalf("provision result: %s", raw)
	}
	if results[1]["type"] != "tool.result" {
		raw, _ := json.Marshal(results[1])
		t.Fatalf("tool result: %s", raw)
	}
	payload, _ := results[1]["payload"].(map[string]any)
	if st, _ := payload["status"].(string); st != "succeeded" {
		t.Fatalf("status=%v", payload["status"])
	}
	text, _ := payload["result_text"].(string)
	if !strings.Contains(text, "Demo") {
		t.Fatalf("result_text=%q", text)
	}
	if w.IsLocalAgent("mb_01h00000000000000000000002") {
		t.Fatal("member must not be local agent")
	}
}

func TestDialerRunConnectsAndExitsOnCancel(t *testing.T) {
	var hits atomic.Int32
	accepted := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workgroups/ws" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "done")
		ctx := r.Context()
		var hello map[string]any
		if err := wsjson.Read(ctx, c, &hello); err != nil {
			return
		}
		_ = wsjson.Write(ctx, c, map[string]any{
			"type": "session.welcome",
			"payload": map[string]any{
				"node_id":               "node_b",
				"connection_generation": 1,
				"schema_version":        "0.5.0",
			},
		})
		select {
		case accepted <- struct{}{}:
		default:
		}
		<-ctx.Done()
	}))
	defer srv.Close()

	w := NewWorker(Config{NodeID: "node_b"})
	d := &Dialer{ManageURL: srv.URL, NodeID: "node_b", Worker: w}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run(ctx, nil)
	}()

	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("did not connect")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("Run err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit")
	}
	if hits.Load() < 1 {
		t.Fatalf("hits=%d", hits.Load())
	}
}

func TestDialerRunReconnectsAfterServerDrop(t *testing.T) {
	var hits atomic.Int32
	second := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workgroups/ws" {
			http.NotFound(w, r)
			return
		}
		n := hits.Add(1)
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx := r.Context()
		var hello map[string]any
		if err := wsjson.Read(ctx, c, &hello); err != nil {
			_ = c.Close(websocket.StatusNormalClosure, "bye")
			return
		}
		_ = wsjson.Write(ctx, c, map[string]any{
			"type": "session.welcome",
			"payload": map[string]any{
				"node_id":               "node_b",
				"connection_generation": n,
				"schema_version":        "0.5.0",
			},
		})
		if n == 1 {
			// 第一轮：欢迎后立刻断线，迫使 Run 重连
			_ = c.Close(websocket.StatusGoingAway, "drop")
			return
		}
		select {
		case second <- struct{}{}:
		default:
		}
		time.Sleep(300 * time.Millisecond)
		_ = c.Close(websocket.StatusNormalClosure, "done")
	}))
	defer srv.Close()

	w := NewWorker(Config{NodeID: "node_b"})
	d := &Dialer{ManageURL: srv.URL, NodeID: "node_b", Worker: w}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var disconnects atomic.Int32
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run(ctx, func(error, time.Duration) { disconnects.Add(1) })
	}()

	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatalf("no reconnect; hits=%d disconnects=%d", hits.Load(), disconnects.Load())
	}
	cancel()
	<-errCh
	if hits.Load() < 2 {
		t.Fatalf("expected >=2 dials, hits=%d", hits.Load())
	}
	if disconnects.Load() < 1 {
		t.Fatalf("expected disconnect callback, got %d", disconnects.Load())
	}
}
