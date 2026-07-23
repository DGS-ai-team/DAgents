package config

import "testing"

func TestExtractWeComWebhookKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"693a91f6-7xxx-4bc4-97a0-0ec2sifa5aaa", "693a91f6-7xxx-4bc4-97a0-0ec2sifa5aaa"},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc-123", "abc-123"},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc-123&foo=1", "abc-123"},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send", ""},
	}
	for _, tc := range cases {
		if got := ExtractWeComWebhookKey(tc.in); got != tc.want {
			t.Fatalf("ExtractWeComWebhookKey(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWeComWebhookKey_prefersExplicitKey(t *testing.T) {
	t.Parallel()
	cfg := &Config{WeCom: WeComConfig{
		WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=from-url",
		WebhookKey: "from-key",
	}}
	if got := cfg.WeComWebhookKey(); got != "from-key" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateWeComConfig_requiresKeyWhenEnabled(t *testing.T) {
	t.Parallel()
	enabled := true
	cfg := &Config{WeCom: WeComConfig{Enabled: &enabled}}
	cfg.ApplyDefaults()
	if err := validateWeComConfig(cfg); err == nil {
		t.Fatal("expected error")
	}
	cfg.WeCom.WebhookKey = "k"
	if err := validateWeComConfig(cfg); err != nil {
		t.Fatal(err)
	}
}
