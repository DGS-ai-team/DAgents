# v1.0.2 发布验收清单

评估日期：2026-10-29。当前正式版本为 v1.0.1（2026-10-28 发布）。本次候选基于 `dev` 的 PR #364 合并提交 `35534b428fdf1813fa8e52d2121720f148856d1f`，版本元数据已准备为 v1.0.2。历史 v1.0.0 验收材料归档在 [release-readiness-v1.0.0.md](archive/release-readiness-v1.0.0.md)。

## 发布范围

v1.0.2 收束本机资源工作台文件链路：Desktop 直接读取本机工作区，Node 提供协调写入与版本校验；大文件使用分段只读预览，草稿保留、外部修改冲突和导出路径受到保护。修复通配地址绑定时 Desktop CLI 的本机连接选择，并避免大型 Markdown 预览占用不断增长的缓存。

本版仍沿用当前发布平台：Windows x64 Desktop 安装包、Linux Node/Client/Browser 助手包和 Manage 离线包；不发布 Windows x86 完整包或 Linux Desktop。

## 已完成验证

- [x] PR #364 合入 `dev`；合入前对应提交 `dea186927a4dcb57ca49dfb3680a107a724c9320` 的必需 CI、Windows x86/x64 构建、Go、Rust、Python、前端、打包检查和 CodeQL 全部通过。
- [x] PR #364 的 13 条 inline bot 评论均已修复并解决；最新 Codex 审查没有未解决发现。
- [x] 桌面前端 80 个测试文件、507 项测试通过，lint 和生产构建通过；Rust 76 项库测试通过；Go 测试、vet、Staticcheck 与本机连接回归通过。
- [x] `python scripts/release/validate_release.py --version 1.0.2` 与 `git diff --check` 通过。

## 发布前仍需完成

- [ ] 发布 PR `dev → main` 的必需 CI 和人工审查通过后合并；保护规则要求人工批准。
- [ ] 按[运维与发布](user/operations.md)完成旧安装升级演练，检查历史、workspace 路径、记忆、工具结果和新安装目录行为，并记录实际候选版本与结果。
- [ ] 合并后确认 `main` 精确指向发布提交，再创建 `v1.0.2` tag。Tag 工作流不得从 `dev` 或其他分支打出。
- [ ] Tag 构建完成后核对 Windows x64 安装包、Linux 助手包、Manage 离线包的版本与内容，并验证 `SHA256SUMS`、`release-manifest.json`、下载和更新选包链路。
- [ ] 发布说明注明 Windows 安装包未做 Authenticode 签名，首次运行时 SmartScreen 可能显示提示。
- [ ] 发布后创建 `main → dev` 同步 PR，避免发布元数据只留在稳定线。

本清单只把已取得的 CI 和本地回归证据标为通过；最终 tag 产物和升级演练完成前，不视为正式发布完成。
