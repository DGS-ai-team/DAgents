package tools

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestLinuxExecAcceptsTerminalConfigID(t *testing.T) {
	addr, fingerprint := startTestSSHServer(t)
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "channel-prod", Host: host, Port: port, Username: "test-user",
			CredentialID: "cred", HostKeyPolicy: "pinned", HostKeyRef: fingerprint,
			RemoteShell: "bash", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "test-secret", Enabled: true},
	}, func(context.Context, string) (string, error) { return "test-password", nil })

	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetAgentID("agent-config-test")
	if err := reg.WithLinuxShellProvider(provider); err != nil {
		t.Fatal(err)
	}
	reg.SetTerminalConfigResolver(fakeTerminalConfigResolver{config: TerminalConfigInfo{
		ConfigID:   TerminalConfigLinuxPrefix + "channel-prod",
		TargetKind: executionTargetLinuxChannel,
		TargetID:   "channel-prod",
	}})

	result, err := reg.Execute(context.Background(), "linux_exec", fmt.Sprintf(`{"config_id":%q,"command":"printf remote-ok"}`, TerminalConfigLinuxPrefix+"channel-prod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "remote-ok") || !strings.Contains(result, "[LINUX_RESULT] exit=0") {
		t.Fatalf("unexpected linux_exec result: %s", result)
	}
}
