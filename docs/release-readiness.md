# v1.0.4 发布验收清单

评估日期：2026-10-30。候选基于 `dev` 合并提交 `02ac1670039949b600d2a3c09cb3db98807feb64`，包含 PR #371 与 #372；正式版本仍为 [v1.0.3](https://github.com/DGS-ai-team/DAgents/releases/tag/v1.0.3)。发布前的版本元数据已更新为 v1.0.4，发布 PR、升级演练、正式 tag 与资产校验尚未完成。

历史 v1.0.2 清单已归档：[release-readiness-v1.0.2.md](archive/releases/release-readiness-v1.0.2.md)。

## 发布范围

v1.0.4 包含工具执行结果字段布局修复，以及工作区文件列表排序、目录/文件标识、路径快捷操作、Word/Markdown/代码/表格预览和 Markdown 源码切换；同时改进输入栏状态提示，并规范 system prompt 中 URL、加粗和特殊字面量的输出格式。

发布平台沿用现行范围：Windows x64 Desktop 安装包、Linux amd64 本地助手包和 Manage 离线包；安装包未签名，发布说明需提醒 SmartScreen 并提供 SHA256 校验文件。

## 已有验证证据

- [x] PR #371 合入 `dev`，对应 CI 全部通过。
- [x] PR #372 合入 `dev`，对应 CI 全部通过，Codex review 无未解决发现。
- [x] PR #372 本地验证：Desktop lint、前端测试（80 个文件、513 项）、生产构建、Rust 格式与测试（77 项）、Node turn Go 测试和 `git diff --check` 通过。
- [x] v1.0.4 版本元数据由 `scripts/release/prepare_release.py` 更新，`scripts/release/validate_release.py --version 1.0.4` 通过。

## 发版门禁与待完成项

- [ ] 对上一正式安装版本执行隔离 QA 升级演练，检查 Agent 历史、workspace 路径、Agent/全局记忆、工具输出文件和新安装不创建 `.runtime/data/`。使用独立 QA AppId 和 workspace 内 fixture，不触碰正式安装目录。
- [ ] 创建并合并 `dev -> main` 发布 PR；发布 PR 必须经过必需 CI 和人工审查。
- [ ] 仅在发布 PR 合并后，从最新 `main` 创建并推送 `v1.0.4` tag。
- [ ] 发布工作流成功后核对 Windows x64、Linux amd64、Manage 包、`SHA256SUMS` 和 `release-manifest.json` 的名称、版本与摘要；完善 Release 说明及 Windows SmartScreen 提示。
- [ ] 使用独立同步分支创建 `main -> dev` 发布后同步 PR，确保 v1.0.4 版本元数据回到开发线。不要把 `main` 本身作为该 PR 的源分支，以免 GitHub 合并后自动删除 main。

当前仓库启用了合并后自动删除源分支。此前 PR #370 用 `main` 作为源分支同步至 `dev`，导致 `main` 被自动删除；已按正式 v1.0.3 tag 恢复 `main`，后续同步必须使用临时源分支。
