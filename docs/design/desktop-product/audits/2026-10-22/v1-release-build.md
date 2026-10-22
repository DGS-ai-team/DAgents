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

## 未完成的实机验收

本记录不代表发行放行。尚无 v1.0.0 tag 或正式 Release。

- 最终候选标准用户安装/升级没有通过。10 月 22 日尝试使用临时标准用户安装官方 v0.11.0 旧包时，Setup 返回退出码 1，没有生成安装目录中的 `bin` 文件；后续诊断命令被本机执行策略以 `blocked by policy` 拒绝，未提供具体规则。QA 安装根目录中的 43 个文件与冷备份逐项哈希一致；当前 18765 Node 仍以原 PID 35692 运行。
- v0.11.0 旧数据的实际备份→升级→恢复及失败恢复演练仍未完成。
- 最终安装候选上的真实 Provider/MCP 配置、Browser 与 Desktop 资源联动仍未完成。
- Auto 24 小时耐久及运行/待审批/空闲状态重启恢复仍未完成。
- 发布 PR 的 Release metadata preflight、必需 CI、CodeQL、独立审批和 main 集成仍未完成。
- 正式 tag 工作流尚未生成最终 `SHA256SUMS` 与 `release-manifest.json`，更新选择和下载链路也未对正式 Release 验证。

标准用户自行卸载测试依照用户此前明确要求跳过，不作为放行条件。Windows 安装包未签名；正式发布时必须在下载页与发布说明标明 SmartScreen 提示。
