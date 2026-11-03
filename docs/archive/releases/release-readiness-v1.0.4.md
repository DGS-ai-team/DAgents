# v1.0.4 发布验收清单

评估日期：2026-11-02。[v1.0.4 已正式发布](https://github.com/DGS-ai-team/DAgents/releases/tag/v1.0.4)。PR #373、#374 已合入 `main`；tag 指向 `0ff031fff624aa2051c17fcde62d2ac502c27dfc`。正式构建和发布资产校验已完成，发布后同步由独立分支提交到 `dev`。

历史 v1.0.2 清单已归档：[release-readiness-v1.0.2.md](archive/releases/release-readiness-v1.0.2.md)。

## 发布范围

v1.0.4 包含工具执行结果字段布局修复，以及工作区文件列表排序、目录/文件标识、路径快捷操作、Word/Markdown/代码/表格预览和 Markdown 源码切换；同时改进输入栏状态提示，规范 system prompt 中 URL、加粗和特殊字面量的输出格式，并修复切换记忆范围时丢失 Agent 类型、workspace 和手册目录的问题。

发布平台沿用现行范围：Windows x64 Desktop 安装包、Linux amd64 本地助手包和 Manage 离线包；安装包未签名，正式发布说明已包含 SmartScreen 提示、下载入口与 SHA256 校验文件。

## 已有验证证据

- [x] PR #371 合入 `dev`，对应 CI 全部通过。
- [x] PR #372 合入 `dev`，对应 CI 全部通过，Codex review 无未解决发现。
- [x] PR #372 本地验证：Desktop lint、前端测试（80 个文件、513 项）、生产构建、Rust 格式与测试（77 项）、Node turn Go 测试和 `git diff --check` 通过。
- [x] v1.0.4 版本元数据由 `scripts/release/prepare_release.py` 更新，`scripts/release/validate_release.py --version 1.0.4` 通过。
- [x] PR #373（v1.0.4 发布准备）已合入 `main`；必需 CI 与 Codex Review 均通过。
- [x] 隔离 Windows 安装/升级/卸载演练通过：旧版 QA 包使用 v1.0.3 标签的安装器源码，并复用 v1.0.3 GitHub Actions run `40992773695` 的四个原始 Windows 二进制；新版 QA 包从当前 v1.0.4 候选构建。两包都使用独立 QA AppId，测试目录位于工作区 fixture。
- [x] 基础演练启动旧版 Node 与 Desktop 后升级到 v1.0.4；安装器退出 Desktop、停止受管 Node，升级后 Node 设置数据库逻辑摘要、listen 配置、运行时哨兵、策略文件和 QA Desktop 设置保持一致。卸载仅移除程序文件，Node 数据库及用户数据仍保留；最终 Verify 通过。
- [x] 演练前后比较当前用户的 Desktop 设置、快捷方式、自启动值、PATH 和正式安装卸载注册项摘要，均未变化；QA 安装没有创建 `.runtime/data`。
- [x] PR #374 已合入 `main`，CI 与 Codex Review 通过。QA runner 使用 `ProcessStartInfo.ArgumentList` 分别传递命令和路径；记忆范围更新仅写入快照的 `defaults`，保留其他元数据。Node API 整包测试和快照元数据回归测试通过。
- [x] 完整数据演练使用旧版 `1.0.3.1-qa` 包安装并通过实际 API 创建自主智能体、生成一轮 Mock 对话、写入 Agent/全局记忆、自定义 workspace 文件和已有工具输出文件 fixture，再升级到从合并提交 `0ff031ff` 构建的 `1.0.4.2-qa` 包。
- [x] 升级时由安装器退出运行中的 Desktop、停止其受管 Node；重启后通过 v1.0.4 API 读回旧对话、自定义 workspace 和 Agent/全局记忆。原始历史 JSONL、workspace 文件和已有工具输出文件摘要保持一致，且没有创建 `.runtime/data`。
- [x] 升级后切换记忆范围，再次 hydrate 并查询 workspace location，确认自主智能体类型和自定义 workspace 路径保持正确。卸载后通过只读 SQLite 查询确认 Agent 元数据、可恢复对话和 Agent/全局记忆内容仍保留，业务文件摘要一致；QA runner 的 Uninstall/Verify 通过，生产设置摘要未变化。
- [x] 新候选 QA 包 SHA256 为 `bfbcd38305f803d10bfe03bc7520a9ad31707d1e55a67d69a059dc6041e6562a`；Node 从上述合并提交重新构建，Desktop 代码与此前候选相同，Client/Browser 复用未变更的 v1.0.3 CI 二进制。该 QA 包只用于隔离验收，不作为正式发布资产。
- [x] 对话测试仅使用 Mock，不调用外部 LLM；工具输出验收覆盖已有文件保留，不代表重新执行了实际工具落盘流程。
- [x] [正式发布工作流 41381880396](https://github.com/DGS-ai-team/DAgents/actions/runs/41381880396) 成功，包含跨语言单元测试、Windows x64、Linux amd64 和 Manage 离线包构建。
- [x] 正式 Release 共五项资产：三个平台/组件包及 `SHA256SUMS`、`release-manifest.json`。逐项核对包名、版本、字节大小，以及 GitHub 上传 SHA256 digest、校验文件与 manifest 三者的摘要一致；manifest 的提交与正式 tag 一致。发布说明已补齐安装与升级提示，并设为 latest。

## 发版门禁与待完成项

- [x] PR #374 已合并，完整数据升级验收通过；最新 `main` 的 CI 成功后创建并推送 `v1.0.4` tag。
- [x] 正式发布工作流成功，资产校验和 Release 说明完善均已完成。
- [ ] 合并独立分支 `codex/sync-main-to-dev-v1.0.4` 提交的发布后同步 PR，将 v1.0.4 版本元数据、快照保留修复和本次验收记录同步到 `dev`。不使用 `main` 本身作为源分支，避免合并后自动删除 main。

当前仓库启用了合并后自动删除源分支。此前 PR #370 用 `main` 作为源分支同步至 `dev`，导致 `main` 被自动删除；已按正式 v1.0.3 tag 恢复 `main`，后续同步必须使用临时源分支。
