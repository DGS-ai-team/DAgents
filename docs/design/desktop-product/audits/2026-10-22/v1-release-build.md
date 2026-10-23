# v1.0.0 候选构建记录

日期：2026-10-22  
候选分支：`codex/release-v1.0.0`  
当前分支头：`e9c36d0c`  
候选构建与 CI 来源提交：`a446490e`。随后提交 `e9c36d0c` 只将打包测试 fixture 的前缀写法简化，产品构建输入未变；简化后的 fixture 在本机重新通过。

## 自动化构建

| 工作流 | 结果 | 说明 |
| --- | --- | --- |
| CI run [39741735618](https://github.com/DGS-ai-team/DAgents/actions/runs/39741735618) | 通过 | Go、Python、Contracts、Tauri、RustSec 与 Packaging 检查通过；手动派发时 Release metadata PR 检查按预期跳过。 |
| Manual Package Binary run [39741735977](https://github.com/DGS-ai-team/DAgents/actions/runs/39741735977) | 通过 | v1.0.0 版本校验、构建前测试、Windows x64 完整安装器、Linux x64 助手包、Manage 离线 bundle 均完成。 |
| Manage bundle 契约测试 | 通过 | 断言离线 bundle 包含 Docker 镜像和安装脚本，但不会再包含已嵌入镜像的第二份 Linux 助手归档。 |
| 本地版本元数据校验 | 通过 | `py -3.13 scripts/release/validate_release.py --version 1.0.0`；发布准备单测 7 项通过。 |

## 产物体积与签名

GitHub Actions 的候选 artifact 大小如下。Windows/Linux 数值是上传 artifact 的大小；Windows 安装器本身在同版本首次候选下载中为 390,904,610 字节，产品版本为 `1.0.0`，Authenticode 状态为 `NotSigned`。完整发布 tag 会重新构建并生成最终 SHA-256 清单。

| 产物 | Artifact 大小 |
| --- | ---: |
| Windows x64 完整安装器 | 390,468,763 字节（约 390 MB） |
| Windows Browser 可执行文件 | 379,702,842 字节（约 380 MB） |
| Linux x64 助手包 | 365,320,909 字节（约 365 MB） |
| Manage 离线 bundle | 425,806,628 字节（约 426 MB） |

Browser/Chromium 单体解释了 Windows 完整包大部分体积。旧正式版 v0.11.0 Windows 安装器为 60,558,443 字节。Manage bundle 修复前的候选 artifact 为 791,188,524 字节；修复后为 425,806,628 字节，减少 365,381,896 字节（约 46%）。镜像仍内置 Linux 助手归档，Manage 启动时继续将其 seed 到下载库；移除的只有离线 bundle 外层那份没有消费者的重复副本。

## 未完成的实机验收与用户豁免

本记录不代表发行放行。尚无 v1.0.0 tag 或正式 Release。

10 月 22 日补充的本地回归：`go test ./internal/api ./internal/browser` 通过；Desktop `test:frontend` 为 76 个文件、466 项通过，含共享图标 7 项；`test:ui` 85 项通过；release metadata 校验及 7 项 release-validator 单测通过；Manage bundle 契约测试通过。当前开发 Node 上，已保存的 DeepSeek profile 调用 `/v1/llm/probe-models` 成功返回 2 个模型，没有发送聊天内容或修改配置。该实例报告版本 `dev`，只能证明当前 profile 的模型列表可读，不能替代最终 v1.0.0 包的 Provider 对话验收。

- 用户已明确要求本次跳过安装流程验证，包括最终候选安装/升级与 v0.11.0 旧数据迁移演练。该流程仍未验证，不视为通过或候选测试失败；10 月 22 日安装尝试没有安装候选文件，QA 安装根目录中的 43 个文件与冷备份逐项哈希一致，当前 18765 Node 仍以原 PID 35692 运行。此前诊断启动被本机执行策略以 `blocked by policy` 拒绝，未提供具体规则。
- 当前 Node 配置包含 DeepSeek profile、`mock=false`；轻量模型列表探测已通过，但将测试消息写入现有对话会污染用户数据，故未发送真实聊天。Node MCP 配置为空（0 个服务），无法做外部 MCP 连接验收。
- 当前 Node 的 Browser 功能未启用，18766 sidecar 未监听；最终 Browser 与 Desktop 资源联动仍未验收。
- 用户已明确要求本次跳过 Auto 测试。Auto 耐久及运行/待审批/空闲状态重启恢复仍未验证，不视为通过或失败，也不作为本次放行阻断项。
- 发布 PR 的 Release metadata preflight、必需 CI、CodeQL、独立审批和 main 集成仍未完成。
- 正式 tag 工作流尚未生成最终 `SHA256SUMS` 与 `release-manifest.json`，更新选择和下载链路也未对正式 Release 验证。

标准用户自行卸载测试依照用户此前明确要求跳过；本轮用户另明确要求跳过整个安装流程验证和 Auto 耐久测试，以上项目均保持未验证记录。Windows 安装包未签名；正式发布时必须在下载页与发布说明标明 SmartScreen 提示。
