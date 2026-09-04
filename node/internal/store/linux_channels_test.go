package store

import (
	"context"
	"strings"
	"testing"
)

func TestLinuxChannelStoreRoundTripKeepsSecretsOpaque(t *testing.T) {
	st, err := OpenLinuxChannels(t.TempDir() + "/linux.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	if err := st.SaveCredential(ctx, LinuxCredentialRecord{
		CredentialID: "cred-prod",
		DisplayName:  "生产 SSH key",
		AuthType:     "private_key",
		SecretRef:    "env:PROD_SSH_KEY",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveChannel(ctx, LinuxChannelRecord{
		ChannelID:     "prod-app-01",
		DisplayName:   "生产应用 01",
		Host:          "10.10.0.21",
		Port:          22,
		Username:      "deploy",
		CredentialID:  "cred-prod",
		HostKeyPolicy: "pinned",
		HostKeyRef:    "SHA256:test-fingerprint",
		RemoteShell:   "bash",
		DefaultCWD:    "/srv/app",
		Enabled:       true,
	}); err != nil {
		t.Fatal(err)
	}

	channel, err := st.GetChannel(ctx, "prod-app-01")
	if err != nil || channel == nil {
		t.Fatalf("channel=%+v err=%v", channel, err)
	}
	if channel.Host != "10.10.0.21" || channel.DefaultCWD != "/srv/app" || channel.Enabled != true {
		t.Fatalf("channel=%+v", channel)
	}
	cred, err := st.GetCredential(ctx, "cred-prod")
	if err != nil || cred == nil {
		t.Fatalf("credential=%+v err=%v", cred, err)
	}
	if cred.SecretRef != "env:PROD_SSH_KEY" || cred.AuthType != "private_key" {
		t.Fatalf("credential=%+v", cred)
	}
	if _, err := st.ResolveLinuxChannel(ctx, "missing"); err == nil {
		t.Fatal("missing channel should fail closed")
	}
	resolved, err := st.ResolveLinuxChannel(ctx, "prod-app-01")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.CredentialID != "cred-prod" || resolved.HostKeyRef != "SHA256:test-fingerprint" {
		t.Fatalf("resolved=%+v", resolved)
	}
	if err := st.SaveBinding(ctx, LinuxChannelBindingRecord{
		AgentID: "agent-a", ChannelID: "prod-app-01", Enabled: true,
		RemoteCWD: "/srv/agent-a", Shell: "bash", AllowedCommands: []string{"git *"},
	}); err != nil {
		t.Fatal(err)
	}
	binding, err := st.ResolveLinuxBinding(ctx, "agent-a", "prod-app-01")
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Enabled || binding.RemoteCWD != "/srv/agent-a" || len(binding.AllowedCommands) != 1 {
		t.Fatalf("binding=%+v", binding)
	}
	configs, err := st.ListTerminalConfigs(ctx, "agent-a")
	if err != nil || len(configs) != 1 {
		t.Fatalf("configs=%+v err=%v", configs, err)
	}
	if configs[0].ConfigID != "linux_channel:prod-app-01" || configs[0].Host != "10.10.0.21" || configs[0].Username != "deploy" || configs[0].Remark != "生产应用 01" {
		t.Fatalf("config=%+v", configs[0])
	}
	resolvedConfig, err := st.ResolveTerminalConfig(ctx, "agent-a", configs[0].ConfigID)
	if err != nil || resolvedConfig.TargetKind != "linux_channel" || resolvedConfig.TargetID != "prod-app-01" {
		t.Fatalf("resolved config=%+v err=%v", resolvedConfig, err)
	}
	if _, err := st.ResolveTerminalConfig(ctx, "agent-b", configs[0].ConfigID); err == nil {
		t.Fatal("unbound agent should not resolve terminal config")
	}
	if _, err := st.ResolveLinuxBinding(ctx, "agent-b", "prod-app-01"); err == nil {
		t.Fatal("unbound agent should fail closed")
	}
	if err := st.DeleteCredential(ctx, "cred-prod"); err == nil {
		t.Fatal("credential in use should not be deleted")
	}
	if err := st.ReplaceBindings(ctx, "agent-a", []LinuxChannelBindingRecord{{AgentID: "agent-a"}}); err == nil {
		t.Fatal("invalid replacement should fail")
	}
	if _, err := st.ResolveLinuxBinding(ctx, "agent-a", "prod-app-01"); err != nil {
		t.Fatalf("failed replacement should preserve existing binding: %v", err)
	}
	if err := st.DeleteChannel(ctx, "prod-app-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResolveLinuxBinding(ctx, "agent-a", "prod-app-01"); err == nil {
		t.Fatal("deleting a channel should remove its agent bindings")
	}
	if err := st.DeleteCredential(ctx, "cred-prod"); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxChannelStoreRejectsUnsafeOrIncompleteConfig(t *testing.T) {
	st, err := OpenLinuxChannels(t.TempDir() + "/linux.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SaveCredential(ctx, LinuxCredentialRecord{
		CredentialID: "cred", AuthType: "password", SecretRef: "env:SSH_PASSWORD", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveChannel(ctx, LinuxChannelRecord{
		ChannelID: "unsafe", Host: "example", Username: "root", CredentialID: "cred",
		HostKeyPolicy: "insecure", Enabled: true,
	}); err == nil {
		t.Fatal("insecure host key policy should be rejected")
	}
	if err := st.SaveCredential(ctx, LinuxCredentialRecord{
		CredentialID: "bad", AuthType: "token", SecretRef: "env:TOKEN", Enabled: true,
	}); err == nil {
		t.Fatal("unsupported auth type should be rejected")
	}
}

func TestLinuxChannelStoreGeneratesUniqueIDs(t *testing.T) {
	st, err := OpenLinuxChannels(t.TempDir() + "/linux.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	credentialID1, err := st.GenerateCredentialID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	credentialID2, err := st.GenerateCredentialID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if credentialID1 == credentialID2 || !strings.HasPrefix(credentialID1, "cred_") || !strings.HasPrefix(credentialID2, "cred_") {
		t.Fatalf("credential ids=%q,%q", credentialID1, credentialID2)
	}
	channelID1, err := st.GenerateChannelID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	channelID2, err := st.GenerateChannelID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if channelID1 == channelID2 || !strings.HasPrefix(channelID1, "channel_") || !strings.HasPrefix(channelID2, "channel_") {
		t.Fatalf("channel ids=%q,%q", channelID1, channelID2)
	}
}

func TestLinuxChannelStoreEncryptsLiteralSecrets(t *testing.T) {
	st, err := OpenLinuxChannels(t.TempDir() + "/linux.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	plain := "password with spaces / 中文"
	ref, err := st.EncryptLiteralSecret(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ref, plain) || !strings.HasPrefix(ref, "literal:") {
		t.Fatalf("literal reference leaks plaintext: %q", ref)
	}
	if err := st.SaveCredential(ctx, LinuxCredentialRecord{
		CredentialID: "direct", AuthType: "password", SecretRef: ref, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ResolveSecret(ctx, ref)
	if err != nil || got != plain {
		t.Fatalf("encrypted secret=%q err=%v", got, err)
	}
}
