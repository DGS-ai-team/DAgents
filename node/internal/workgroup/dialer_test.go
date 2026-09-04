package workgroup

import (
	"context"
	"net/http"
	"net/http/httptest"
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
