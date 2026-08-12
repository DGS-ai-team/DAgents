package policy

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var policySeedCandidates = []string{
	"packaging/runtime/policy",
	"../packaging/runtime/policy",
	"../../packaging/runtime/policy",
	"../../../packaging/runtime/policy",
}

var requiredPolicyFiles = []string{
	"tool.approval.txt",
	filepath.Join("shell", "bash.approval.txt"),
	filepath.Join("shell", "cmd.approval.txt"),
	filepath.Join("shell", "powershell.approval.txt"),
}

// EnsureRuntimePolicy 确保 `.runtime/policy` 目录与策略文件存在，并把种子中缺失的工具模式合并进已有 tool.approval.txt。
//
// 逻辑：
// 1. 创建 policy 与 shell 子目录；
// 2. 缺失文件时从 `packaging/runtime/policy` 种子目录复制；
// 3. 种子不可用时创建空文件（保守默认：未命中条目需审批）；
// 4. 对已有 tool.approval.txt 仅追加种子缺项（不覆盖用户已改模式）。
//
// 副作用：可能创建目录并写入策略文件。
func EnsureRuntimePolicy(runtimeDir string) error {
	_, err := MergeMissingSeedIntoRuntimePolicy(runtimeDir)
	return err
}

// ensureRuntimePolicyFiles 只负责目录与缺失文件补齐（不含种子缺项合并）。
func ensureRuntimePolicyFiles(runtimeDir string) error {
	policyDir := filepath.Join(runtimeDir, "policy")
	shellDir := filepath.Join(policyDir, "shell")
	if err := os.MkdirAll(shellDir, 0o755); err != nil {
		return fmt.Errorf("create policy dir: %w", err)
	}
	seedDir := findSeedPolicyDir()
	for _, rel := range requiredPolicyFiles {
		dst := filepath.Join(policyDir, rel)
		if _, err := os.Stat(dst); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat policy file %q: %w", dst, err)
		}
		if seedDir != "" {
			src := filepath.Join(seedDir, rel)
			if _, err := os.Stat(src); err == nil {
				if err := copyFile(src, dst); err != nil {
					return err
				}
				continue
			}
		}
		if err := os.WriteFile(dst, []byte(""), 0o644); err != nil {
			return fmt.Errorf("create empty policy file %q: %w", dst, err)
		}
	}
	return nil
}

func findSeedPolicyDir() string {
	for _, candidate := range policySeedCandidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open seed policy %q: %w", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create policy file %q: %w", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy policy file to %q: %w", dst, err)
	}
	return nil
}

// LoadRuntime 从 `<runtime>/policy` 加载 txt 策略并确保文件存在。
func LoadRuntime(runtimeDir string) (*Engine, error) {
	runtimeDir = strings.TrimSpace(runtimeDir)
	if runtimeDir == "" {
		runtimeDir = "./.runtime"
	}
	if err := EnsureRuntimePolicy(runtimeDir); err != nil {
		return nil, err
	}
	return loadFromDir(filepath.Join(runtimeDir, "policy"))
}

// LoadFromDir 从 policy 目录加载 txt 策略（单测与工具脚本）。
func LoadFromDir(policyDir string) (*Engine, error) {
	return loadFromDir(policyDir)
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
