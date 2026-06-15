# `docs/design/`

架构决策、实施计划与 **Agent Node 优化** 文档。阅读顺序建议：**[major-changes.md](./major-changes.md)**（实录）→ 精简专题 → 深度分析 / 设计稿。

---

## 优化实录与专题（优先）

| 文件 | 状态 | 说明 |
|------|------|------|
| [major-changes.md](./major-changes.md) | 持续更新 | **重大变更索引**；每条：背景 / 思路 / 落地 / 局限 |
| [tool-context-cost-analysis.md](./tool-context-cost-analysis.md) | ✅ 已落地 | 工具链成本（WS1/3/5/6）；**四段结构范本** |
| [context-compression-cache-analysis.md](./context-compression-cache-analysis.md) | ✅ 已落地 | 压缩 × Prompt Cache 深度分析（M1–M3） |
| [tool-before-hook-duplicate-approval.md](./tool-before-hook-duplicate-approval.md) | ✅ 已落地 | `tool.before_each` + 重复调用审批（WS6） |
| [skills-context-cost-analysis.md](./skills-context-cost-analysis.md) | ⏸ 搁置 | WS4 skills schema；分析存档 |

---

## 架构与路线（ADR）

| 文件 | 说明 |
|------|------|
| [background-and-motivation.md](./background-and-motivation.md) | 老旧 OS 动机与 Go 方案 |
| [three-component-model.md](./three-component-model.md) | Node / Client / Manage 边界 |
| [agent-client-refactor-plan.md](./agent-client-refactor-plan.md) | AC 分步计划 N0–N7 |
| [manage-architecture.md](./manage-architecture.md) | Manage 统一控制面（取代 `register_center/`） |
| [agent-directory-phase1.md](./agent-directory-phase1.md) | RC 企业化草案（过渡，见 Manage） |

---

## 设计稿（未完全落地）

| 文件 | 说明 |
|------|------|
| [agent-hooks.md](./agent-hooks.md) | Hook 框架总纲；**部分落地**（`tool.before_each` / `tool.after_each`） |
| [ux-agent-owned-file-approval.md](./ux-agent-owned-file-approval.md) | 写操作审批信任链 |

---

## 文档分工

```text
major-changes.md     → 可读摘要 + 条目模板（onboarding）
*-analysis.md（精简） → 痛点 / 分析 / 思路 / 方案（<150 行为宜）
*-analysis.md（深度） → 实现回溯、手算、历史备选（可长）
*-approval.md 等     → 单特性完整设计记录（落地后保留供 review）
```

新增大项优化：先追加 **major-changes** 条目，再按需写精简专题；勿在 PR 描述里单独留档。
