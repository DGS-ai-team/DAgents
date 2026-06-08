# OfficeCLI（第三方，仅 Windows 发布包）

**Windows** 发布包（zip / `.exe` 安装包）内置 **[OfficeCLI](https://github.com/iOfficeAI/OfficeCLI)**，用于 Agent 通过 CLI 创建、分析与修改 Office 文档（`.docx` / `.xlsx` / `.pptx`）。Linux tarball **不含** OfficeCLI。

| 项 | 值 |
|---|---|
| 上游仓库 | https://github.com/iOfficeAI/OfficeCLI |
| 许可证 | [GNU AGPL v3.0](https://github.com/iOfficeAI/OfficeCLI/blob/main/LICENSE) |
| 二进制路径 | `.runtime/scripts/officecli.exe` |
| 对应 skills | `.runtime/skills/officecli*`（由上游 `skills/` 目录同步） |

## 使用

```cmd
REM 安装后（PATH 已含 .runtime\scripts）
officecli --version

REM 便携解压包（未 install 时）
.runtime\scripts\officecli.exe --version
```

在 DAgents 中通过 `/skill load officecli`（或 `officecli-docx` / `officecli-pptx` / `officecli-xlsx` 等子 skill）加载规则后，Agent 会调用 `bash_run` 执行上述命令。

## 版本

打包脚本默认 pin **`OFFICECLI_VERSION=v1.0.106`**（可在 CI 覆盖）。实际版本见 `.runtime/scripts/.officecli-version`。

## 修改与再分发

OfficeCLI 以 AGPL-3.0 发布。若你修改其二进制或随产品再分发，须遵守 AGPL 义务（提供对应源代码等）。DAgents 本体与 OfficeCLI 为独立组件；合规责任由分发方自行评估。
