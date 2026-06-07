package tools

import (
	"os/exec"
	"regexp"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

var (
	suLoginWithCRe         = regexp.MustCompile(`(?i)^\s*(?:/[\w./-]+/)?su\b\s+(?:-|(?:-l\b)|(?:--login\b))\s+(?:[^\s/-][^\s]*)\s+(?:-c|--command)\b`)
	sudoFamilyRe           = regexp.MustCompile(`(?i)^\s*(?:/[\w./-]+/)?(?:sudo|sudoedit)\b`)
	sudoNonInteractiveFlag = regexp.MustCompile(`(?i)(?:^|\s)-n(?:\s|$)|(?:^|\s)--non-interactive(?:\s|$)`)
)

// blockedNonRootPasswordPromptingShell 判定非 root bash 是否应拦截 su/sudo。

// 逻辑对齐 Python `_blocked_non_root_password_prompting_shell`。
func blockedNonRootPasswordPromptingShell(command string, st shellType) string {
	if st != shellBash {
		return ""
	}
	snap := hostsnapshot.Get()
	if snap.OSKind == "windows" {
		return ""
	}
	if snap.EffectiveUID == nil || *snap.EffectiveUID == 0 {
		return ""
	}
	for _, segment := range policy.SplitBashStatements(command) {
		s := strings.TrimSpace(segment)
		if s == "" {
			continue
		}
		if suLoginWithCRe.MatchString(s) {
			return "ERROR: 当前进程非 root，不允许执行 `su - <user> -c ...` 形式的跨用户登录 shell。"
		}
		if sudoFamilyRe.MatchString(s) {
			if sudoNonInteractiveFlag.MatchString(s) {
				continue
			}
			return "ERROR: 当前进程非 root，不允许执行可能提示输入密码的 `sudo`/`sudoedit`（片段中缺少 `-n` 或 `--non-interactive`）。"
		}
	}
	return ""
}

// killShellProcess 终止 shell 子进程；POSIX 下先向进程组发 SIGTERM。
func killShellProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	signalKillProcessGroup(cmd)
	_ = cmd.Process.Kill()
}
