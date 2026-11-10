# v1.0.6 发布验收清单

评估日期：2026-11-10。当前整理 `dev` 发布候选元数据，基线提交为 `0cb1a1ae`；尚未创建发布 PR、正式 tag 或 GitHub Release。上一版验收清单已归档到 [v1.0.5 发布验收清单](archive/releases/release-readiness-v1.0.5.md)。

## 发布范围

候选包含 PR [#386](https://github.com/DGS-ai-team/DAgents/pull/386) 的 OpenAI 兼容 HTTP 连接池复用，以及 PR [#387](https://github.com/DGS-ai-team/DAgents/pull/387) 的工具审批、`max_tool_calls` 限额、工作区引用与 Linux SSH 凭据改进。另包括候选工作区中的桌面公共控件样式收敛、设置页统一分组与行布局、折叠区文案和说明层级调整。

正式资产仍按 [发布流程](release-process.md) 生成：Windows x64 Inno 安装包、Linux amd64 Node 本地助手包、Manage 离线包，以及 SHA256SUMS 和 `release-manifest.json`。Windows 安装包未签名；本清单不表示资产已构建或发布。

## 已验证

- [x] PR #386：Go LLM/turn 定向测试、Staticcheck 和 Linux race CI 通过；CI run `41676792061` 全通过。验证覆盖连接池复用、并发边界和请求间凭据/模型隔离；未调用真实付费模型。Windows 本地 race 因 CGo 编译失败未启动。
- [x] PR #387：`scripts/verify.ps1` 在 2026-11-09 完整通过；PR 描述记录 Node、Manage、Go、Rust、Desktop 的质量门与 CI 通过，以及工具审批、限额、资源引用、Linux SSH 私钥和 known_hosts 的专项验证。
- [x] 最终候选全量前端验证通过：Vitest 622/622、公共图标检查 7/7、UI 测试 120/120 和 Desktop lint；深色原生下拉箭头修复的控件专项测试与定向 lint 通过。
- [x] Windows x64 debug native 构建通过，日志见 `.runtime-test-settings-disclosure/pr-native-1.0.6.log`。原生验收已覆盖设置页宽窄窗口、折叠标题、开关行、侧边栏右键菜单和键盘操作；最新深色原生下拉箭头仅有定向测试，尚未在原生窗口目视确认。构建含 Vite chunk 与 `STATIC_VCRUNTIME` 既有警告。
- [x] `scripts/release/prepare_release.py` 已生成 1.0.6 的版本元数据、CHANGELOG、README、手册及路线图版本标记。
- [x] 本地 `scripts/release/validate_release.py --version 1.0.6` 通过。

## 发布门禁

- [ ] 在 Windows 原生窗口目视确认深色原生下拉箭头。
- [ ] 使用一次性隔离环境完成真实 v1.0.5→v1.0.6 安装升级与数据保留验证，并覆盖 Agent 历史、workspace、Agent/全局记忆、工具结果文件及新安装不创建 `data/`。不得对日常运行目录或真实用户数据执行安装演练。
- [ ] 创建 `dev -> main` 发布 PR；等待必需 CI、Release metadata 预检和审查通过后合入。
- [ ] 只在合入后的最新 `main` 提交创建 `v1.0.6` tag，不覆盖已有 tag。
- [ ] 正式 Release workflow 成功，检查 Windows 安装包、Linux 包、Manage 离线包、SHA256SUMS 和 manifest 的版本、提交、文件大小与校验值。
- [ ] 补齐正式下载说明并由维护者确认 GitHub Release 为 latest；之后创建 `main -> dev` 同步 PR。

## 升级提示

Node 与 Desktop 应配套升级。工具执行上限由 `max_tool_calls` 统一控制（默认 64）；旧 `max_steps`、`max_tool_rounds` 和子智能体 `max_turns` 设置不再生效。升级前备份完整 `.runtime`，包括 `.llm_secret_key`。首次连接未知 SSH 主机时应核对服务器指纹再保存为信任项。

## 验收边界

候选 PR 的 CI 与已记录专项验证不等于正式发布 PR 的门禁结果。深色原生下拉箭头目视确认、真实跨版本隔离升级演练、发布工作流和 GitHub Release 尚待完成；完成前不宣称安装包已发布或生产升级已验证。

## 合并前升级演练建议

请按[Windows 安装与升级验收](../packaging/windows/install-upgrade-acceptance.md)执行一次性隔离演练，并使用真实 v1.0.5 与 v1.0.6 候选包验证跨版本升级、卸载及应用数据保留。QA 脚本的同 bundle 模拟升级不能替代这项跨版本验收。目前尚未执行，未触碰任何运行目录。
