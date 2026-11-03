# v1.0.5 发布验收清单

评估日期：2026-11-03。当前为发布候选，尚未创建正式 tag 或 GitHub Release。功能与修复 PR [#383](https://github.com/DGS-ai-team/DAgents/pull/383) 已合入 dev（1de1d1f5），本发布分支用于将其合入 main。

历史 v1.0.4 清单归档至 `docs/archive/releases/release-readiness-v1.0.4.md`；本次具体修改与验证边界见 [功能减负与实现收敛验收](audit/2026-11-03-simplification-and-acceptance.md)。

## 发布范围

统一设置框架、选择器、上下文菜单和保存反馈；保留工具执行中的实时记录、文字扫光与垂直切换，由输入栏统一停止。移除临时岗位授权、命令帮助、Desktop/Node 扩展包上传及未使用的状态链和 CLI 包装。模型参数按供应商能力配置，模型档案统一持久化，修复旧配置迁移失败后的数据保护。恢复子智能体进度，优化长对话流式更新和冷会话状态读取。Manage 仓库保留，普通用户仅显示浏览入口。另含 v1.0.4 以来的浏览器进程清理、终端选择器、浏览器导航、代码块和资源菜单打磨。

正式资产范围：Windows x64 Inno 安装包、Linux amd64 本地助手包、Manage 离线包，以及 SHA256SUMS 和 release-manifest.json。Windows 安装包未签名。

## 已完成证据

- [x] 版本元数据更新为 1.0.5，`python scripts/release/validate_release.py --version 1.0.5` 通过。
- [x] Desktop 84 个文件、570 项 Vitest，7 项图标契约与 106 项 UI 状态检查通过；后续重试短文案的定向测试 29 项及 lint 通过。lint/build 和 Windows 原生构建通过。
- [x] Manage lint/build、Ruff、Pyright 和 212 项 Python unittest 通过；隔离数据库分别验证管理员发布与普通身份浏览。
- [x] Go 1.26.10 五模块 test/vet/staticcheck、Node/client 构建；Rust fmt 和 77 项测试；品牌、API fixture 和 OpenAPI 契约通过。
- [x] 原生工具运行验收使用隔离的本地 OpenAI SSE fixture 和真实 PowerShell 命令。输入栏停止后取得 cancelled_by_user、工具 cancelled、运行工具数 0；临时 Agent、档案与 fixture 已清理。
- [x] 旧凭据迁移失败时普通设置保存和模型切换均被拒绝，原配置保留；补齐环境变量重启后两档模型及密钥恢复。旧 Qwen effort 归一化后，合法 thinking、预算及密钥保留，新提交的非法参数继续拒绝。
- [x] CodeQL/Codex 两轮发现均修复并补充回归；提交 922cfaf3 的 Node 全包测试与 staticcheck 通过。新增工作区引用组件交互、旧 OpenAI effort、清空边界和持久化错误回归。
- [x] 发布候选补充模型故障恢复：Node 全包测试与 staticcheck 通过；本地故障接口验证暂时/持续 503、401、退避中取消、超长 Retry-After、部分输出后断流，均收敛终态；后续消息成功，清空对话有效，部分正文进入下一轮上下文。
- [x] 原生窗口验证失败提示、重试进度、输入和模型选择解锁、资源页悬浮错误提示。外部付费模型未调用，测试使用隔离目录和本地故障接口。

## 发布门禁

- [x] PR #383 提交 922cfaf3 的完整 CI（run 41619773131）、Linux race、CodeQL 均通过；2026-11-03 10:35 UTC 的机器人复审完成且无发现，历史讨论均已解决。
- [x] PR #383 于 2026-11-03 合入 dev，合并提交 1de1d1f56eeed0d9754a3f9156c6180f462d8878。
- [x] v1.0.4 到 v1.0.5 的隔离安装、升级、卸载演练通过，验证历史、工作区、Agent/全局记忆、工具文件和用户数据保留。
- [ ] 从最新 dev 建立独立发布分支，更新本清单并通过 main 发布 PR 的 CI、Release metadata 与审查。
- [ ] 仅在合入后的最新 origin/main 提交创建 v1.0.5 tag，不覆盖任何已有 tag。
- [ ] 正式工作流成功；五项资产齐全，版本、提交、大小与 SHA256 在上传 digest、SHA256SUMS、manifest 间一致。
- [ ] Release 说明补齐下载入口、数据备份、旧环境变量、未签名/SmartScreen 与校验提示，确认 latest。
- [ ] 通过临时源分支提交 main 到 dev 的发布后同步 PR，避免自动删除 main。

## 验收边界

本次原生联调使用本地 SSE fixture，不代表验证了外部真实付费模型、真实 SSH 和跨机器工作组。Windows race 受本机工具链限制，合并以 CI Linux race 结果为准。冷状态缓存仍需查询持久日志版本，不改变完整恢复时的日志重放。

升级前备份完整 `.runtime`（包括 `.llm_secret_key`），保留旧凭据所在的进程环境变量。本版安装演练使用原 v1.0.4 CI 二进制与提交 922cfaf3 的候选构建，QA 安装包版本 1.0.5.1，独立 AppId。原安装器前后数据库 hash 保留，启动迁移后另建卸载前后基线；已验证已知业务字段和 active-only 结构，没有迁移前完整 settings_json 快照，因此不声称逐字段完全等价。
