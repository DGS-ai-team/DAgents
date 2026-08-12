# OfficeCLI（第三方，需自行安装）

**[OfficeCLI](https://github.com/iOfficeAI/OfficeCLI)** 用于 Agent 通过 CLI 创建、分析与修改 Office 文档（`.docx` / `.xlsx` / `.pptx`）。**DAgents 发布包不含此工具**；见 **[`../RECOMMENDED_CLI_TOOLS.md`](../RECOMMENDED_CLI_TOOLS.md)**。

| 项 | 值 |
|---|---|
| 上游仓库 | https://github.com/iOfficeAI/OfficeCLI |
| 许可证 | [GNU AGPL v3.0](https://github.com/iOfficeAI/OfficeCLI/blob/main/LICENSE) |
| 建议路径 | `<运行根>/.runtime/externaltools/officecli` 或 `officecli.exe` |
| 对应 skills | 上游 `skills/officecli*` → 复制到 `<运行根>/.runtime/skills/` |

## 安装

1. 打开 [Releases](https://github.com/iOfficeAI/OfficeCLI/releases)，下载与平台匹配的二进制。
2. 放入 **`.runtime/externaltools/`**（或任意已在 `PATH` 中的目录）。
3. 从上游仓库 **`skills/`** 目录复制 `officecli*` 至 **`.runtime/skills/`**。

```bash
# Linux / macOS 示例
curl -L -o .runtime/externaltools/officecli \
  "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.106/officecli-linux-x64"
chmod +x .runtime/externaltools/officecli
```

```cmd
REM Windows 示例：下载 officecli-win-x64.exe 并重命名
copy officecli-win-x64.exe .runtime\externaltools\officecli.exe
```

## 使用

```bash
officecli --version
# 便携路径
.runtime/externaltools/officecli --version
```

在 DAgents 中通过 **`/skill load officecli`**（或 `officecli-docx` / `officecli-pptx` / `officecli-xlsx` 等子 skill）加载规则后，Agent 会调用 `bash_run` 执行上述命令。

## 修改与再分发

OfficeCLI 以 AGPL-3.0 发布。若你修改其二进制或随产品再分发，须遵守 AGPL 义务（提供对应源代码等）。DAgents 本体与 OfficeCLI 为独立组件；合规责任由分发方自行评估。
