# `docs/design/`

架构决策与 **Agent Node + Client（AC）** 实施计划。

| 文件 | 说明 |
|------|------|
| [background-and-motivation.md](./background-and-motivation.md) | 动机：老旧 OS、Go 运行时 |
| [three-component-model.md](./three-component-model.md) | Node / Client / Manage 边界 |
| [agent-client-refactor-plan.md](./agent-client-refactor-plan.md) | N0–N7  checklist 与里程碑 |
| [agent-directory-phase1.md](./agent-directory-phase1.md) | **Phase 1** Register Center 企业化（**将被 Manage Registry 吸收**） |
| [manage-architecture.md](./manage-architecture.md) | **Manage 统一控制面**：Registry + A2A + Skills + Platform；取代 `register_center/` |
| [agent-hooks.md](./agent-hooks.md) | **Agent Hook 扩展点**（设计稿）：turn 全链路阶段锚点、`HookRegistry`、配置与落地顺序 |
