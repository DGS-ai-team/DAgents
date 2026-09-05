package workgroup

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	agentIDHeader = "x-dagents-agent-id"
)

// Dialer 连接 Manage `/v1/workgroups/ws`，hello/resume 后分发业务帧。
type Dialer struct {
	ManageURL      string // http(s)://host:port
	NodeID         string
	Worker         *Worker
	WorkgroupID    string   // 可选：单组 resume.offer
	WorkgroupIDs   []string // 静态多组订阅
	ListWorkgroups func(ctx context.Context) ([]string, error)
	OnRealtime     func(map[string]any)

	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool
}

// WSURL 将 Manage HTTP base 转为工作组 WS URL。
func WSURL(manageURL string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(manageURL), "/")
	if base == "" {
		return "", errf(CodeSchemaMismatch, "manage URL required")
	}
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	case strings.HasPrefix(base, "ws://"), strings.HasPrefix(base, "wss://"):
		// ok
	default:
		return "", errf(CodeSchemaMismatch, "unsupported manage URL scheme")
	}
	return base + "/v1/workgroups/ws", nil
}

// Run 在 ctx 有效期内反复 ConnectAndServe：Manage 晚于 Node 启动、重启或短暂断线时
// 能自动重连并 resume outbox，避免成员永久等待 Node 会话建立。
// onDisconnect 在每次会话结束后、退避等待前调用（可为 nil）。
func (d *Dialer) Run(ctx context.Context, onDisconnect func(err error, backoff time.Duration)) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := d.ConnectAndServe(ctx)
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if onDisconnect != nil {
			onDisconnect(err, backoff)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// ConnectAndServe 拨号、hello、可选 resume，然后阻塞读循环直至 ctx 取消或连接关闭。
// 单次会话；生产路径请用 Run 以获得断线重连。
func (d *Dialer) ConnectAndServe(ctx context.Context) error {
	if d.Worker == nil || d.NodeID == "" {
		return errf(CodeSchemaMismatch, "worker/node_id required")
	}
	wsURL, err := WSURL(d.ManageURL)
	if err != nil {
		return err
	}
	hdr := http.Header{}
	hdr.Set(agentIDHeader, d.NodeID)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		return fmt.Errorf("workgroup ws dial: %w", err)
	}
	d.mu.Lock()
	d.conn = conn
	d.closed = false
	d.mu.Unlock()
	defer d.Close()
	var writeMu sync.Mutex
	// Agent session events are asynchronous: the local Agent runtime can keep
	// producing deltas after the read loop has accepted the start command.
	d.Worker.SetOutbound(func(frame map[string]any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return wsjson.Write(ctx, conn, frame)
	})
	defer d.Worker.SetOutbound(nil)
	stopCloseOnCancel := make(chan struct{})
	defer close(stopCloseOnCancel)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.CloseNow()
		case <-stopCloseOnCancel:
		}
	}()

	cs := &ClientSession{Worker: d.Worker, OnRealtime: d.OnRealtime}
	hello := cs.BuildHello()
	if err := wsjson.Write(ctx, conn, hello); err != nil {
		return err
	}

	// 等待 welcome（允许中间夹杂其它控制消息，最多读几帧）
	welcomed := false
	for i := 0; i < 8 && !welcomed; i++ {
		var loose map[string]any
		if err := wsjson.Read(ctx, conn, &loose); err != nil {
			return fmt.Errorf("read welcome: %w", err)
		}
		if t, _ := loose["type"].(string); t == "session.welcome" {
			payload, _ := loose["payload"].(map[string]any)
			cs.ApplyWelcome(payload)
			welcomed = true
		}
	}
	if !welcomed {
		return errf(CodeConflict, "session.welcome not received")
	}

	if err := d.sendResumeOffers(ctx, conn); err != nil {
		return err
	}

	go func() {
		// 定期刷新订阅列表并 resume，避免「Manage 侧新建成员后 Node 已在线却不拉 pending」
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				writeMu.Lock()
				_ = d.sendResumeOffers(ctx, conn)
				writeMu.Unlock()
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		res, err := cs.HandleIncomingJSON(data)
		if res != nil && res.AckEnvelope != nil {
			writeMu.Lock()
			werr := wsjson.Write(ctx, conn, res.AckEnvelope)
			writeMu.Unlock()
			if werr != nil {
				return fmt.Errorf("write ack envelope: %w", werr)
			}
			if cerr := d.Worker.CommitPendingAck(res); cerr != nil {
				return fmt.Errorf("commit delivery ack: %w", cerr)
			}
		}
		if err != nil && res != nil && res.ErrorCode == CodeFencingRejected {
			// 旧帧：已回 session.error，继续读
			continue
		}
		if err != nil {
			writeMu.Lock()
			werr := wsjson.Write(ctx, conn, map[string]any{
				"type": "session.error",
				"payload": map[string]any{
					"code":    string(CodeConflict),
					"message": err.Error(),
				},
			})
			writeMu.Unlock()
			if werr != nil {
				return fmt.Errorf("write session.error: %w", werr)
			}
		}
	}
}

// resolveResumeWorkgroups 合并静态配置与动态订阅列表（去重保序）。
func (d *Dialer) resolveResumeWorkgroups(ctx context.Context) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add(d.WorkgroupID)
	for _, id := range d.WorkgroupIDs {
		add(id)
	}
	if d.ListWorkgroups != nil {
		ids, err := d.ListWorkgroups(ctx)
		if err == nil {
			for _, id := range ids {
				add(id)
			}
		}
		// 列表失败时仍用静态配置继续，避免阻断连线
	}
	return out
}

func (d *Dialer) sendResumeOffers(ctx context.Context, conn *websocket.Conn) error {
	for _, wg := range d.resolveResumeWorkgroups(ctx) {
		cur := d.Worker.Session.OfferResumeFor(wg)
		offer := map[string]any{
			"type": "resume.offer",
			"payload": map[string]any{
				"workgroup_id":          wg,
				"last_ack_delivery_seq": cur.LastAckDeliverySeq,
			},
		}
		if err := wsjson.Write(ctx, conn, offer); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭底层连接。
func (d *Dialer) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	conn := d.conn
	d.conn = nil
	d.mu.Unlock()
	if conn != nil {
		// Dialer shutdown is an internal lifecycle operation. A graceful
		// close handshake can block when Manage disappears or the peer does
		// not answer, which in turn delays Run(ctx) cancellation. Tear down
		// the socket immediately; reconnects will establish a fresh session.
		_ = conn.CloseNow()
	}
}

// Ping 可选保活（测试/运维）。
func (d *Dialer) Ping(ctx context.Context) error {
	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()
	if conn == nil {
		return errf(CodeNotAuthorized, "not connected")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Ping(ctx)
}
