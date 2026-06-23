package turn

import (
	"path/filepath"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

// SyncLoadedSkillHooks 按当前 loaded skills 同步 skill 级 plugin hooks（load/unload/clear 后调用）。
func (o *Orchestrator) SyncLoadedSkillHooks(loaded []skills.LoadedSkill) {
	if o == nil || o.toolHooks == nil || o.skillAccess.Catalog == nil {
		return
	}
	o.toolHooks.RemovePhaseHooksByPrefix(hooks.SkillHookNamePrefix)
	if len(loaded) == 0 {
		return
	}
	root := o.skillAccess.Catalog.Root()
	if root == "" {
		return
	}
	for _, sk := range loaded {
		skillName := strings.TrimSpace(sk.SkillName)
		if skillName == "" {
			continue
		}
		hooksDir := filepath.Join(root, skillName, "hooks")
		_ = hooks.LoadSkillPluginsFromDir(o.toolHooks, hooksDir, skillName, o.logger)
	}
}
