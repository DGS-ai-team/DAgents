package tools

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestNewSSHClientConnContextCancellationClosesHandshake(t *testing.T) {
	clientConn, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, _, err := newSSHClientConnContext(ctx, clientConn, "pipe", &ssh.ClientConfig{
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         time.Minute,
		})
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancelled SSH handshake to fail")
		}
	case <-time.After(time.Second):
		t.Fatal("SSH handshake did not stop after context cancellation")
	}
}
