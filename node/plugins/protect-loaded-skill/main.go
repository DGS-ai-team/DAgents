// protect-loaded-skill 可 build 为 skill 级 .so，与 Node 内置 builtin.loaded_skill_file_guard 同逻辑。
package main

import (
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
)

func Register(reg *hooks.PluginRegistrar) error {
	reg.Register(hooks.NewLoadedSkillFileGuardHook(), hooks.RegisterOpts{
		Priority: 15,
		Timeout:  hooks.DefaultInlineHookTimeout,
		OnError:  hooks.OnErrorContinue,
	})
	return nil
}
