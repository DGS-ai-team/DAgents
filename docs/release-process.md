# DAgents 发布与分支规范

## 分支职责

- `dev` 是日常集成线：功能分支通过 Pull Request 合入，持续接收开发中的变更。
- `main` 是稳定发布线：只接收经过验证的发布 Pull Request，或经过审查的紧急 hotfix。
- `vX.Y.Z` 标签只能从 `main` 创建。发布工作流会拒绝指向其他提交或 force-push 产生的标签；仓库设置还必须禁止 tag 删除和覆盖。

当前仓库默认分支仍是 `dev`，以保持日常开发入口稳定；完成一次正式发布并确认 `main` 的同步、回滚和升级流程后，再单独评估是否切换仓库默认分支。

## 正式发版流程

1. 在 `dev` 完成功能开发和验收，使用 `scripts/release/prepare_release.py` 生成版本元数据。
2. 使用 `scripts/release/validate_release.py` 本地检查 `VERSION`、所有包元数据、`CHANGELOG.md`、`README.md`、手册和路线图是否一致。
3. 创建 `dev -> main` 的发布 Pull Request；针对 `main` 的 PR 会额外运行 Release metadata 预检，等待必需 CI 和审查通过后合并。
4. 在合并后的 `main` 最新提交上创建并推送 `vX.Y.Z` 标签。
5. `build-and-release.yml` 校验标签、`VERSION` 和所有包元数据，并构建 GitHub Release、Node 本地助手包、Windows 安装器和 Manage 离线包；发布中同时生成 `SHA256SUMS` 和 `release-manifest.json`。
6. 发布成功后工作流自动创建 `main -> dev` 同步 PR（不自动合并），避免发布提交（版本号、CHANGELOG 或 hotfix）在开发线上丢失。

因此，发版时应该把经过验证的 `dev` 合入 `main`，但不应该让每次开发提交自动同步到 `main`，也不应该让发布工作流自动替用户完成分支合并。发布 PR 是版本冻结、审查和回滚边界。

## 版本来源

正式版本只维护一个语义来源：

```text
VERSION  ->  构建时注入 / 包元数据  ->  CHANGELOG.md  ->  vX.Y.Z tag
```

构建脚本和工作流默认读取根目录 `VERSION`，正式发布必须校验 tag、`VERSION`、包元数据和构建产物完全一致。手工打包用于调试和验收，不等同于正式发布；`skip_tests` 只能用于排查打包脚本，不能用于发版。

发版工作流只由 `v*` 标签触发。`dev`、`main` 的提交检查统一由 `ci-required.yml` 负责，因此不会因为 main push 再额外重复运行一套完整发布单测。

## Hotfix

紧急修复可以从 `main` 创建 hotfix 分支，完成审查后直接合入 `main`，再递增补丁版本并打标签。发布后必须把 `main` 的 hotfix 和版本元数据同步回 `dev`。

## 合并策略

`dev` 和 `main` 统一使用 Squash merge，保证一个功能或一个发布 PR 对应一个清晰提交；已合并分支由 GitHub 自动删除。保护规则要求 Pull Request、必需 CI 和审查，发布标签规则禁止覆盖或删除已有标签。
