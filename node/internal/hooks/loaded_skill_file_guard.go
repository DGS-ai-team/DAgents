package hooks

import (
	"context"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// LoadedSkillFileDenyMessage 为拦截修改已加载 skill 文件时的 tool 结果文案。
const LoadedSkillFileDenyMessage = "已加载的技能文件不允许修改"

const loadedSkillFileGuardPriority = 15

// LoadedSkillFileGuardHook 阻止 write_file / search_replace / bash_run 修改 loaded skill 目录内文件。
type LoadedSkillFileGuardHook struct{}

// NewLoadedSkillFileGuardHook 构造 write-skill 配套的 loaded skill 文件保护 Hook。
func NewLoadedSkillFileGuardHook() Hook {
	return LoadedSkillFileGuardHook{}
}

func (LoadedSkillFileGuardHook) Name() string { return "builtin.loaded_skill_file_guard" }

func (LoadedSkillFileGuardHook) Phases() []Phase { return []Phase{PhaseToolBeforeEach} }

func (h LoadedSkillFileGuardHook) Run(ctx context.Context, hc *Context, host Host) (Result, error) {
	return runToolBeforeEachHook(ctx, hc, host, h.Name(), func(_ context.Context, in ToolBeforeEachInput, out *ToolBeforeEachResult) error {
		if out == nil {
			return nil
		}
		loaded := loadedSkillNamesFromContext(hc, host)
		if len(loaded) == 0 {
			return nil
		}
		if shouldBlockLoadedSkillFileModify(in.ToolName, in.ToolArgs, loaded) {
			out.Action = policy.ActionDeny
			out.ApprovalReason = LoadedSkillFileDenyMessage
		}
		return nil
	})
}

func loadedSkillNamesFromContext(hc *Context, host Host) []string {
	if hc != nil && len(hc.LoadedSkills) > 0 {
		return loadedSkillNamesFromInfo(hc.LoadedSkills)
	}
	if host != nil {
		snap := host.Snapshot()
		if len(snap.LoadedSkills) > 0 {
			return loadedSkillNamesFromInfo(snap.LoadedSkills)
		}
	}
	return nil
}

func loadedSkillNamesFromInfo(items []LoadedSkillInfo) []string {
	if len(items) == 0 {
		return nil
	}
	names := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, sk := range items {
		name := strings.TrimSpace(sk.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	return names
}

func shouldBlockLoadedSkillFileModify(toolName string, args map[string]any, loadedNames []string) bool {
	if len(loadedNames) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "write_file", "search_replace":
		path := WriteToolRelPath(toolName, args)
		return path != "" && relPathTouchesLoadedSkill(path, loadedNames)
	case "bash_run":
		cmd, _ := args["command"].(string)
		return bashCommandMayModifyLoadedSkill(cmd, loadedNames)
	default:
		return false
	}
}

func relPathTouchesLoadedSkill(relPath string, loadedNames []string) bool {
	key := normalizeSkillRelPath(relPath)
	if key == "" {
		return false
	}
	for _, name := range loadedNames {
		if pathUnderLoadedSkill(key, name) {
			return true
		}
	}
	return false
}

func normalizeSkillRelPath(relPath string) string {
	key := NormalizePathKey(relPath)
	key = strings.TrimPrefix(key, "./")
	for strings.HasPrefix(key, "../") {
		key = strings.TrimPrefix(key, "../")
	}
	key = strings.TrimPrefix(key, ".runtime/")
	return key
}

func pathUnderLoadedSkill(key, skillName string) bool {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return false
	}
	prefix := "skills/" + skillName + "/"
	if strings.HasPrefix(key, prefix) {
		return true
	}
	if key == "skills/"+skillName {
		return true
	}
	return false
}

func bashCommandMayModifyLoadedSkill(command string, loadedNames []string) bool {
	cmd := strings.ToLower(strings.ReplaceAll(command, "\\", "/"))
	if strings.TrimSpace(cmd) == "" || !bashCommandLooksMutating(cmd) {
		return false
	}
	for _, name := range loadedNames {
		seg := "skills/" + strings.ToLower(strings.TrimSpace(name))
		if strings.Contains(cmd, seg) {
			return true
		}
	}
	return false
}

func bashCommandLooksMutating(cmd string) bool {
	padded := " " + cmd + " "
	markers := []string{
		">", ">>",
		" set-content ", " add-content ", " out-file ", " clear-content ", " new-item ",
		" move-item ", " copy-item ", " remove-item ", " rename-item ",
		" rm ", " mv ", " cp ", " del ", " erase ",
		" sed -i", " tee ", " sponge ",
	}
	for _, m := range markers {
		if strings.Contains(padded, m) {
			return true
		}
	}
	return false
}

// ToolDenyMessage 返回 tool 拒绝时的结果文案。
func ToolDenyMessage(decision ToolBeforeEachResult) string {
	if msg := strings.TrimSpace(decision.ApprovalReason); msg != "" {
		return msg
	}
	return "rejected: policy_denied"
}
