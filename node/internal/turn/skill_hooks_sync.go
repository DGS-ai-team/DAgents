package turn

import (
	"path/filepath"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

// SkillHookSyncFailure identifies one skill whose plugin hooks did not all
// register. The skill body can still be model-visible; hook registration is a
// separate, explicitly reported capability.
type SkillHookSyncFailure struct {
	SkillName string `json:"skill_name"`
	Path      string `json:"path,omitempty"`
	Error     string `json:"error"`
}

// SkillHooksSyncResult is the authoritative result of synchronizing the
// currently loaded skill hooks. Status is synchronized when every attempted
// skill directory completed, partial when at least one plugin failed, and
// unavailable when the orchestrator has no hook registry/catalog.
type SkillHooksSyncResult struct {
	Status string                 `json:"status"`
	Loaded []string               `json:"loaded,omitempty"`
	Failed []SkillHookSyncFailure `json:"failed,omitempty"`
}

// SyncLoadedSkillHooks 按当前 loaded skills 同步 skill 级 plugin hooks（load/unload/clear 后调用）。

func (o *Orchestrator) SyncLoadedSkillHooks(loaded []skills.LoadedSkill) SkillHooksSyncResult {
	result := SkillHooksSyncResult{Status: "synchronized"}
	if o == nil || o.toolHooks == nil || o.skillAccess.Catalog == nil {
		result.Status = "unavailable"
		return result
	}
	o.toolHooks.RemovePhaseHooksByPrefix(hooks.SkillHookNamePrefix)
	if len(loaded) == 0 {
		return result
	}
	root := o.skillAccess.Catalog.Root()
	if root == "" {
		result.Status = "unavailable"
		return result
	}
	for _, sk := range loaded {
		skillName := strings.TrimSpace(sk.SkillName)
		if skillName == "" {
			continue
		}
		directoryName := strings.TrimSpace(sk.DirectoryName)
		if directoryName == "" {
			directoryName = skillName
		}
		hooksDir := filepath.Join(root, directoryName, "hooks")
		pluginResult := hooks.LoadSkillPluginsFromDirDetailed(o.toolHooks, hooksDir, skillName, o.logger)
		for _, path := range pluginResult.Loaded {
			result.Loaded = append(result.Loaded, filepath.Base(path))
		}
		for _, failure := range pluginResult.Failed {
			result.Status = "partial"
			result.Failed = append(result.Failed, SkillHookSyncFailure{
				SkillName: skillName,
				Path:      filepath.Base(failure.Path),
				Error:     failure.Error,
			})
		}
	}
	return result
}
