# v1.0.4 发布验收清单

评估日期：2026-11-02。v1.0.4 候选已由 PR #373 合入 `main`，当前合并提交为 `e51d275e6c725a755fdb548bb9d259077844fded`；上一正式版本仍为 [v1.0.3](https://github.com/DGS-ai-team/DAgents/releases/tag/v1.0.3)。正式 tag、发布资产校验和发布后同步仍待完成。

历史 v1.0.2 清单已归档：[release-readiness-v1.0.2.md](archive/releases/release-readiness-v1.0.2.md)。

## 发布范围

v1.0.4 包含工具执行结果字段布局修复，以及工作区文件列表排序、目录/文件标识、路径快捷操作、Word/Markdown/代码/表格预览和 Markdown 源码切换；同时改进输入栏状态提示，并规范 system prompt 中 URL、加粗和特殊字面量的输出格式。

发布平台沿用现行范围：Windows x64 Desktop 安装包、Linux amd64 本地助手包和 Manage 离线包；安装包未签名，发布说明需提醒 SmartScreen 并提供 SHA256 校验文件。

## 已有验证证据

- [x] PR #371 合入 `dev`，对应 CI 全部通过。
- [x] PR #372 合入 `dev`，对应 CI 全部通过，Codex review 无未解决发现。
- [x] PR #372 本地验证：Desktop lint、前端测试（80 个文件、513 项）、生产构建、Rust 格式与测试（77 项）、Node turn Go 测试和 `git diff --check` 通过。
- [x] v1.0.4 版本元数据由 `scripts/release/prepare_release.py` 更新，`scripts/release/validate_release.py --version 1.0.4` 通过。
- [x] PR #373（v1.0.4 发布准备）已合入 `main`；必需 CI 与 Codex Review 均通过。
- [x] 隔离 Windows 安装/升级/卸载演练通过：旧版 QA 包使用 v1.0.3 标签的安装器源码，并复用 v1.0.3 GitHub Actions run `40992773695` 的四个原始 Windows 二进制；新版 QA 包从当前 v1.0.4 候选构建。两包都使用独立 QA AppId，测试目录位于工作区 fixture。
- [x] 基础演练启动旧版 Node 与 Desktop 后升级到 v1.0.4；安装器退出 Desktop、停止受管 Node，升级后 Node 设置数据库逻辑摘要、listen 配置、运行时哨兵、策略文件和 QA Desktop 设置保持一致。卸载仅移除程序文件，Node 数据库及用户数据仍保留；最终 Verify 通过。
- [x] 演练前后比较当前用户的 Desktop 设置、快捷方式、自启动值、PATH 和正式安装卸载注册项摘要，均未变化；QA 安装没有创建 `.runtime/data`。
- [x] 演练中修正 QA runner 的 CLI 启动方式：使用 `ProcessStartInfo.ArgumentList` 分别传递命令和路径，避免空格路径下把多个 CLI 参数合并成单个参数并意外启动 Desktop。该修复已在 QA Uninstall/Verify 阶段通过验证，待随后续修复 PR 合入。
- [x] 使用 v1.0.3 Node 的 Mock 配置通过实际 API 创建 Agent、生成一轮持久化对话，并写入 Agent/全局记忆和自定义 workspace 文件，确认旧版接口能够读回这些数据。
- [x] 上述数据准备过程发现候选代码在切换 Agent 记忆范围时会重建不完整的配置快照，丢失 workspace、Agent 类型和手册目录；已加入快照元数据回归测试并修复，定向 Go 测试通过。
- [ ] 用含 Agent 历史、workspace 路径、Agent/全局记忆及工具结果文件的旧版 fixture 完成一次安装器升级，再从 v1.0.4 Node 读回并核对这些数据。基础安装器演练不覆盖此项。
- [x] QA 演练不包含模型对话测试；使用本地启动和持久化检查，不调用外部 LLM。

## 发版门禁与待完成项

- [ ] 合并 QA runner 参数传递修复及 Agent 记忆范围切换时保留完整快照的修复，完成上面的数据升级验收后，再从最新 `main` 创建并推送 `v1.0.4` tag。
- [ ] 发布工作流成功后核对 Windows x64、Linux amd64、Manage 包、`SHA256SUMS` 和 `release-manifest.json` 的名称、版本与摘要；完善 Release 说明及 Windows SmartScreen 提示。
- [ ] 使用独立同步分支创建 `main -> dev` 发布后同步 PR，确保 v1.0.4 版本元数据回到开发线。不要把 `main` 本身作为该 PR 的源分支，以免 GitHub 合并后自动删除 main。

当前仓库启用了合并后自动删除源分支。此前 PR #370 用 `main` 作为源分支同步至 `dev`，导致 `main` 被自动删除；已按正式 v1.0.3 tag 恢复 `main`，后续同步必须使用临时源分支。
