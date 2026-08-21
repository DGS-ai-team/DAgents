package tools

import (
	"os"
	"strings"
	"testing"
)

func TestShouldScrubShellEnvironmentName(t *testing.T) {
	for _, name := range []string{"OPENAI_API_KEY", "TENCENT_DOC_KEY", "DB_PASSWORD", "SSH_AUTH_SOCK", "DAGENTS_RUNTIME"} {
		if !shouldScrubShellEnvironmentName(name) {
			t.Fatalf("expected %q to be scrubbed", name)
		}
	}
	for _, name := range []string{"PATH", "HOME", "LANG", "TERM"} {
		if shouldScrubShellEnvironmentName(name) {
			t.Fatalf("did not expect %q to be scrubbed", name)
		}
	}
}

func TestScrubbedShellEnvironmentRemovesInheritedSecretsAndAllowsExplicitSet(t *testing.T) {
	t.Setenv("DAgents_TEST_SECRET", "inherited")
	t.Setenv("DA_TEST_PATH", "safe")
	env := scrubbedShellEnvironment(map[string]string{"DAgents_TEST_SECRET": "explicit"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "DAgents_TEST_SECRET=inherited") {
		t.Fatal("inherited secret leaked into shell environment")
	}
	if !strings.Contains(joined, "DAgents_TEST_SECRET=explicit") {
		t.Fatal("explicit environment value was not preserved")
	}
	if os.Getenv("DA_TEST_PATH") == "" || !strings.Contains(joined, "DA_TEST_PATH=safe") {
		t.Fatal("ordinary environment value was unexpectedly removed")
	}
}

func TestEnvironmentPolicyModes(t *testing.T) {
	t.Setenv("DA_ENV_POLICY_SECRET", "inherited-secret")
	t.Setenv("DA_ENV_POLICY_ALLOWED", "allowed")
	t.Setenv("DA_ENV_POLICY_OTHER", "other")

	inherited, err := buildShellEnvironment(nil, EnvironmentPolicy{Mode: EnvironmentPolicyInherit})
	if err != nil || !strings.Contains(strings.Join(inherited, "\n"), "DA_ENV_POLICY_SECRET=inherited-secret") {
		t.Fatalf("inherit policy=%v err=%v", inherited, err)
	}
	scrubbed, err := buildShellEnvironment(nil, EnvironmentPolicy{Mode: EnvironmentPolicyScrub})
	if err != nil || strings.Contains(strings.Join(scrubbed, "\n"), "DA_ENV_POLICY_SECRET=inherited-secret") {
		t.Fatalf("scrub policy leaked secret: %v err=%v", scrubbed, err)
	}
	allowed, err := buildShellEnvironment(nil, EnvironmentPolicy{
		Mode:  EnvironmentPolicyAllow,
		Allow: []string{"DA_ENV_POLICY_ALLOWED"},
	})
	allowedText := strings.Join(allowed, "\n")
	if err != nil || !strings.Contains(allowedText, "DA_ENV_POLICY_ALLOWED=allowed") || strings.Contains(allowedText, "DA_ENV_POLICY_OTHER=other") {
		t.Fatalf("allow policy=%v err=%v", allowed, err)
	}
	setOnly, err := buildShellEnvironment(map[string]string{"DA_ENV_POLICY_EXTRA": "extra"}, EnvironmentPolicy{
		Mode: EnvironmentPolicySet,
		Set:  map[string]string{"DA_ENV_POLICY_SET": "set"},
	})
	setText := strings.Join(setOnly, "\n")
	if err != nil || !strings.Contains(setText, "DA_ENV_POLICY_SET=set") || !strings.Contains(setText, "DA_ENV_POLICY_EXTRA=extra") || strings.Contains(setText, "DA_ENV_POLICY_OTHER=other") {
		t.Fatalf("set policy=%v err=%v", setOnly, err)
	}
}

func TestEnvironmentPolicyRejectsInvalidConfiguration(t *testing.T) {
	if _, err := buildShellEnvironment(nil, EnvironmentPolicy{Mode: "unknown"}); err == nil {
		t.Fatal("expected unknown policy mode to fail")
	}
	if _, err := buildShellEnvironment(nil, EnvironmentPolicy{Mode: EnvironmentPolicySet, Set: map[string]string{"BAD-NAME": "x"}}); err == nil {
		t.Fatal("expected invalid set variable name to fail")
	}
	if _, err := buildShellEnvironment(nil, EnvironmentPolicy{Mode: EnvironmentPolicyAllow, Allow: []string{"BAD-NAME"}}); err == nil {
		t.Fatal("expected invalid allow variable name to fail")
	}
}
