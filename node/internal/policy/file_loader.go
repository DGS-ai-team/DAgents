package policy

import (
	"os"
	"path/filepath"
)

var policySeedCandidates = []string{
	"packaging/runtime/policy",
	"../packaging/runtime/policy",
	"../../packaging/runtime/policy",
	"../../../packaging/runtime/policy",
}

// LoadFromDir loads a policy directory for focused tests and local tooling.
// Production Agent policy is stored in agents.db and uses NewEngineFromMaps.
func LoadFromDir(policyDir string) (*Engine, error) {
	return loadFromDir(policyDir)
}

func findSeedPolicyDir() string {
	for _, candidate := range policySeedCandidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func loadFromDir(policyDir string) (*Engine, error) {
	toolModes, err := parseEntryFile(filepath.Join(policyDir, "tool.approval.txt"), ModeRule)
	if err != nil {
		return nil, err
	}
	shellModes := map[ShellType]map[string]ApprovalMode{}
	for _, item := range []struct {
		shell ShellType
		file  string
	}{
		{ShellBash, "bash.approval.txt"},
		{ShellCmd, "cmd.approval.txt"},
		{ShellPowerShell, "powershell.approval.txt"},
	} {
		mapping, err := parseEntryFile(filepath.Join(policyDir, "shell", item.file), ModeRule)
		if err != nil {
			return nil, err
		}
		shellModes[item.shell] = mapping
	}
	return &Engine{
		toolModes:  toolModes,
		shellModes: shellModes,
		policyDir:  policyDir,
	}, nil
}
