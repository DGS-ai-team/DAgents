# v1.0.2 发布验收清单

评估日期：2026-10-29。当前正式版本为 v1.0.1（2026-10-28 发布）。v1.0.2 候选基于 `dev` 的 PR #364 合并提交 `35534b428fdf1813fa8e52d2121720f148856d1f`；发布 PR #365 已合入 `main`，发布提交为 `dd11eabb0fd89439a259fb905169f7efee81634a`。历史 v1.0.0 验收材料归档在 [release-readiness-v1.0.0.md](archive/release-readiness-v1.0.0.md)。

## 发布范围

v1.0.2 收束本机资源工作台文件链路：Desktop 直接读取本机工作区，Node 提供协调写入与版本校验；大文件使用分段只读预览，草稿保留、外部修改冲突和导出路径受到保护。修复通配地址绑定时 Desktop CLI 的本机连接选择，并避免大型 Markdown 预览占用不断增长的缓存。

本版仍沿用当前发布平台：Windows x64 Desktop 安装包、Linux Node/Client/Browser 助手包和 Manage 离线包；不发布 Windows x86 完整包或 Linux Desktop。

## 已完成验证

- [x] PR #364 合入 `dev`；合入前对应提交 `dea186927a4dcb57ca49dfb3680a107a724c9320` 的必需 CI、Windows x86/x64 构建、Go、Rust、Python、前端、打包检查和 CodeQL 全部通过。
- [x] PR #364 的 13 条 inline bot 评论均已修复并解决；最新 Codex 审查没有未解决发现。
- [x] 桌面前端 80 个测试文件、507 项测试通过，lint 和生产构建通过；Rust 76 项库测试通过；Go 测试、vet、Staticcheck 与本机连接回归通过。
- [x] `python scripts/release/validate_release.py --version 1.0.2` 与 `git diff --check` 通过。
- [x] 旧安装升级演练通过：使用隔离 QA 安装器从 v1.0.1.1 升级到 v1.0.2.1；安装器正常关闭旧 Desktop/Node，升级后 Node 数据库逻辑内容、配置、策略、桌面设置和运行时哨兵均保持一致。
- [x] 升级后启动 v1.0.2 Node/Desktop，确认 Agent 历史对话、Agent 与全局记忆、两个 Agent 的 workspace 路径、workspace 文件和工具输出文件均保留；全新安装未创建 `.runtime/data`。演练使用 Mock LLM，不调用外部模型。
- [x] 正式 tag `v1.0.2` 指向 `main` 发布提交 `dd11eabb0fd89439a259fb905169f7efee81634a`；发布工作流 run [40833816356](https://github.com/DGS-ai-team/DAgents/actions/runs/40833816356) 全部成功。
- [x] GitHub Release 的 `release-manifest.json`、`SHA256SUMS` 与三个正式包的 GitHub SHA256 摘要一致：Windows x64 安装器、Linux amd64 本地助手包和 Manage 离线包。
- [x] Release 说明已列出下载入口，并说明 Windows 安装包未签名、首次运行可能出现 SmartScreen 提示及安装前核对校验和。

## 发布后同步

- [ ] 将 `main` 同步回 `dev` 的发布后 PR 经 CI 与人工审查后合并，确保 v1.0.2 元数据和验收记录进入开发线。

正式 Release 已发布。同步 PR 合并后，本清单与开发线完成闭环。
