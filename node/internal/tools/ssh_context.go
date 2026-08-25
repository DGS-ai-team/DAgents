package tools

import (
	"context"
	"net"

	"golang.org/x/crypto/ssh"
)

// newSSHClientConnContext makes the SSH handshake obey the caller's context.
// ssh.NewClientConn only observes ClientConfig.Timeout; without closing the
// underlying socket on cancellation, a cancelled terminal_command can remain
// in the handshake until the full connect timeout and hold the channel slot.
func newSSHClientConnContext(
	ctx context.Context,
	conn net.Conn,
	addr string,
	config *ssh.ClientConfig,
) (ssh.Conn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if conn == nil {
		return nil, nil, nil, context.Canceled
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	clientConn, chans, requests, err := ssh.NewClientConn(conn, addr, config)
	close(done)
	return clientConn, chans, requests, err
}
